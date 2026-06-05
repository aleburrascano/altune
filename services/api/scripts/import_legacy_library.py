"""One-shot import of songs from the legacy Supabase database into Altune.

Usage:
    uv run python scripts/import_legacy_library.py \
        --old-supabase-url https://arjfyxmvhzwhfwxgnrjg.supabase.co \
        --old-supabase-key <secret_key> \
        --old-user-id c5d0d898-1b52-43a0-80b5-47a25f03ffb6 \
        --new-user-id <altune_supabase_auth_uuid> \
        [--dry-run]

The script fetches all songs for the given old user, maps them to Altune's
Track schema, and bulk-inserts via INSERT ... ON CONFLICT DO NOTHING.
Idempotent — safe to run multiple times.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
import urllib.request
from dataclasses import dataclass
from datetime import UTC, datetime
from uuid import UUID, uuid4

# AIDEV-NOTE: inline copy of the domain dedup_key normalizer so the script
# is self-contained and can run without the full app installed.
_SEP = "\x1f"


def _norm(value: str) -> str:
    return " ".join(value.split()).casefold()


def _dedup_key(title: str, artist: str, album: str | None) -> str:
    return _SEP.join((_norm(title), _norm(artist), _norm(album or "")))


@dataclass
class ImportRow:
    id: UUID
    user_id: UUID
    title: str
    artist: str
    album: str | None
    duration_seconds: int | None
    added_at: str
    artwork_url: str | None
    acquisition_status: str
    dedup_key: str
    year: int | None
    genre: str | None
    track_number: int | None
    album_artist: str | None
    isrc: str | None
    audio_ref: str | None


def fetch_songs(
    base_url: str,
    api_key: str,
    old_user_id: str,
) -> list[dict[str, object]]:
    """Fetch all songs for the given user from old Supabase, paginated."""
    all_rows: list[dict[str, object]] = []
    offset = 0
    page_size = 1000

    while True:
        url = (
            f"{base_url}/rest/v1/songs"
            f"?user_id=eq.{old_user_id}"
            f"&order=id.asc"
            f"&offset={offset}"
            f"&limit={page_size}"
        )
        headers = {
            "apikey": api_key,
            "Authorization": f"Bearer {api_key}",
            "Accept": "application/json",
            "User-Agent": "altune-import/1.0",
            "Prefer": "count=exact",
        }
        req = urllib.request.Request(url, headers=headers)
        resp = urllib.request.urlopen(req)  # noqa: S310
        data: list[dict[str, object]] = json.loads(resp.read())
        all_rows.extend(data)

        content_range = resp.headers.get("Content-Range", "")
        match = re.search(r"/(\d+)$", content_range)
        total = int(match.group(1)) if match else len(all_rows)

        if len(all_rows) >= total:
            break
        offset += page_size

    return all_rows


_EMAIL_PATH_RE = re.compile(r"^/mnt/oci-music/[^/]+/")


def map_song(song: dict[str, object], new_user_id: UUID) -> ImportRow | None:
    """Map a legacy song row to an Altune ImportRow. Returns None if invalid."""
    title = song.get("title")
    artist = song.get("artist")
    if not title or not artist or not isinstance(title, str) or not isinstance(artist, str):
        return None

    album = song.get("album")
    if album is not None and not isinstance(album, str):
        album = str(album)

    duration_raw = song.get("duration")
    duration_seconds: int | None = None
    if duration_raw is not None:
        try:
            duration_seconds = round(float(str(duration_raw)))
        except (ValueError, TypeError):
            pass

    file_path = song.get("file_path")
    audio_ref: str | None = None
    if isinstance(file_path, str) and file_path:
        relative = _EMAIL_PATH_RE.sub("", file_path)
        audio_ref = f"{new_user_id}/{relative}"

    added_date = song.get("added_date")
    added_at = str(added_date) if added_date else datetime.now(UTC).isoformat()

    year_raw = song.get("year")
    year: int | None = None
    if year_raw is not None:
        try:
            year = int(str(year_raw))
            if year <= 0:
                year = None
        except (ValueError, TypeError):
            pass

    track_number_raw = song.get("track_number")
    track_number: int | None = None
    if track_number_raw is not None:
        try:
            track_number = int(str(track_number_raw))
            if track_number <= 0:
                track_number = None
        except (ValueError, TypeError):
            pass

    return ImportRow(
        id=uuid4(),
        user_id=new_user_id,
        title=title,
        artist=artist,
        album=album if album else None,
        duration_seconds=duration_seconds,
        added_at=added_at,
        artwork_url=str(song["album_art"]) if song.get("album_art") else None,
        acquisition_status="ready" if audio_ref else "pending",
        dedup_key=_dedup_key(title, artist, album),
        year=year,
        genre=str(song["genre"]) if song.get("genre") else None,
        track_number=track_number,
        album_artist=str(song["album_artist"]) if song.get("album_artist") else None,
        isrc=str(song["isrc"]) if song.get("isrc") else None,
        audio_ref=audio_ref,
    )


def _parse_timestamp(value: str) -> datetime:
    """Parse an ISO-ish timestamp string into a timezone-aware datetime."""
    value = value.strip()
    if value.endswith("Z"):
        value = value[:-1] + "+00:00"
    try:
        dt = datetime.fromisoformat(value)
    except ValueError:
        dt = datetime.strptime(value, "%Y-%m-%dT%H:%M:%S")  # noqa: DTZ007
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=UTC)
    return dt


def build_insert_sql(rows: list[ImportRow]) -> tuple[str, list[dict[str, object]]]:
    """Build a bulk INSERT ... ON CONFLICT DO NOTHING statement."""
    cols = [
        "id",
        "user_id",
        "title",
        "artist",
        "album",
        "duration_seconds",
        "added_at",
        "artwork_url",
        "acquisition_status",
        "dedup_key",
        "year",
        "genre",
        "track_number",
        "album_artist",
        "isrc",
        "audio_ref",
    ]
    placeholders = ", ".join(f":{c}" for c in cols)
    sql = (
        f"INSERT INTO tracks ({', '.join(cols)}) "
        f"VALUES ({placeholders}) "
        f"ON CONFLICT (user_id, dedup_key) DO NOTHING"
    )
    params = [
        {
            "id": r.id,
            "user_id": r.user_id,
            "title": r.title,
            "artist": r.artist,
            "album": r.album,
            "duration_seconds": r.duration_seconds,
            "added_at": _parse_timestamp(r.added_at),
            "artwork_url": r.artwork_url,
            "acquisition_status": r.acquisition_status,
            "dedup_key": r.dedup_key,
            "year": r.year,
            "genre": r.genre,
            "track_number": r.track_number,
            "album_artist": r.album_artist,
            "isrc": r.isrc,
            "audio_ref": r.audio_ref,
        }
        for r in rows
    ]
    return sql, params


def main() -> None:
    parser = argparse.ArgumentParser(description="Import legacy library from old Supabase")
    parser.add_argument("--old-supabase-url", required=True)
    parser.add_argument("--old-supabase-key", required=True)
    parser.add_argument("--old-user-id", required=True)
    parser.add_argument("--new-user-id", required=True, help="Altune Supabase auth UUID")
    parser.add_argument("--database-url", help="Altune Postgres URL (or set DATABASE_URL env)")
    parser.add_argument("--dry-run", action="store_true", help="Print mapping without inserting")
    args = parser.parse_args()

    new_user_id = UUID(args.new_user_id)

    print(f"Fetching songs for old user {args.old_user_id}...")
    songs = fetch_songs(args.old_supabase_url, args.old_supabase_key, args.old_user_id)
    print(f"Fetched {len(songs)} songs.")

    mapped: list[ImportRow] = []
    skipped = 0
    for song in songs:
        row = map_song(song, new_user_id)
        if row is None:
            skipped += 1
            print(f"  SKIP (invalid): {song.get('title', '?')} by {song.get('artist', '?')}")
        else:
            mapped.append(row)

    print(f"Mapped: {len(mapped)}, Skipped (invalid): {skipped}")

    if args.dry_run:
        print("\n=== DRY RUN — first 5 mapped rows ===")
        for row in mapped[:5]:
            print(f"  {row.artist} - {row.title}")
            print(f"    album={row.album}, year={row.year}, genre={row.genre}")
            print(f"    audio_ref={row.audio_ref}")
            print(f"    dedup_key={row.dedup_key[:60]}...")
        print(f"\nTotal would insert: {len(mapped)}")
        return

    import asyncio
    import os

    import sqlalchemy
    from sqlalchemy.ext.asyncio import create_async_engine

    db_url = args.database_url or os.environ.get("DATABASE_URL")
    if not db_url:
        print("ERROR: --database-url or DATABASE_URL env required", file=sys.stderr)
        sys.exit(1)

    if not db_url.startswith("postgresql+asyncpg://"):
        db_url = db_url.replace("postgresql://", "postgresql+asyncpg://")

    sql, params = build_insert_sql(mapped)

    async def run_import() -> None:
        engine = create_async_engine(db_url, pool_pre_ping=True)
        inserted = 0
        deduped = 0
        errored = 0

        async with engine.begin() as conn:
            for p in params:
                try:
                    result = await conn.execute(sqlalchemy.text(sql), p)
                    if result.rowcount > 0:
                        inserted += 1
                    else:
                        deduped += 1
                except Exception as exc:
                    errored += 1
                    print(f"  ERROR: {p['title']} by {p['artist']}: {exc}", file=sys.stderr)

        await engine.dispose()
        print(f"\nDone. Inserted: {inserted}, Deduped (skipped): {deduped}, Errored: {errored}")

    asyncio.run(run_import())


if __name__ == "__main__":
    main()
