# ruff: noqa: T201
"""Retroactive dedup cleanup — identify and remove duplicate tracks.

Usage:
    uv run python -m altune.adapters.inbound.cli.dedup_migration              # dry-run
    uv run python -m altune.adapters.inbound.cli.dedup_migration --execute    # apply
"""

from __future__ import annotations

import argparse
import asyncio
import sys

import structlog

log = structlog.get_logger(__name__)

STATUS_PRIORITY = {"ready": 0, "pending": 1, "failed": 2}


async def _run(execute: bool) -> None:
    from sqlalchemy import text

    from altune.platform.config import Settings
    from altune.platform.db import create_engine, create_sessionmaker

    cfg = Settings()  # type: ignore[call-arg]
    if cfg.database_url is None:
        print("ERROR: DATABASE_URL not set")
        sys.exit(1)

    engine = create_engine(str(cfg.database_url))
    sessionmaker = create_sessionmaker(engine)

    groups_found = 0
    tracks_deleted = 0
    playlists_remapped = 0

    try:
        async with sessionmaker() as session:
            dupe_rows = (
                await session.execute(
                    text(
                        "SELECT user_id, dedup_key, "
                        "       array_agg(id ORDER BY "
                        "           CASE acquisition_status "
                        "               WHEN 'ready' THEN 0 WHEN 'pending' THEN 1 ELSE 2 "
                        "           END, added_at ASC"
                        "       ) AS ids "
                        "FROM tracks "
                        "GROUP BY user_id, dedup_key "
                        "HAVING count(*) > 1"
                    )
                )
            ).fetchall()

            groups_found = len(dupe_rows)
            if groups_found == 0:
                print("\nNo duplicates found.\n")
                return

            print(f"\nFound {groups_found} duplicate group(s):\n")

            for row in dupe_rows:
                _user_id, _dedup_key, ids = row
                keep_id = ids[0]
                delete_ids = ids[1:]

                detail_rows = (
                    await session.execute(
                        text(
                            "SELECT id, title, artist, acquisition_status FROM tracks WHERE id = ANY(:ids)"
                        ),
                        {"ids": ids},
                    )
                ).fetchall()

                detail_map = {str(r[0]): r for r in detail_rows}
                keep_detail = detail_map.get(str(keep_id))
                keep_title = keep_detail[1] if keep_detail else "?"
                keep_artist = keep_detail[2] if keep_detail else "?"
                keep_status = keep_detail[3] if keep_detail else "?"

                print(f'  Group: "{keep_title}" — {keep_artist}')
                print(f"    KEEP:   id={keep_id} status={keep_status}")
                for did in delete_ids:
                    d = detail_map.get(str(did))
                    s = d[3] if d else "?"
                    print(f"    DELETE: id={did} status={s}")

                if execute:
                    for did in delete_ids:
                        remapped = (
                            await session.execute(
                                text(
                                    "UPDATE playlist_tracks SET track_id = :keep_id "
                                    "WHERE track_id = :del_id "
                                    "AND playlist_id NOT IN ("
                                    "    SELECT playlist_id FROM playlist_tracks WHERE track_id = :keep_id"
                                    ")"
                                ),
                                {"keep_id": keep_id, "del_id": did},
                            )
                        ).rowcount
                        playlists_remapped += remapped

                        await session.execute(
                            text("DELETE FROM playlist_tracks WHERE track_id = :del_id"),
                            {"del_id": did},
                        )

                        await session.execute(
                            text("DELETE FROM tracks WHERE id = :del_id"),
                            {"del_id": did},
                        )
                        tracks_deleted += 1

                    # Fix contiguous positions in affected playlists
                    affected_playlists = (
                        await session.execute(
                            text(
                                "SELECT DISTINCT playlist_id FROM playlist_tracks "
                                "WHERE track_id = :keep_id"
                            ),
                            {"keep_id": keep_id},
                        )
                    ).fetchall()
                    for (pl_id,) in affected_playlists:
                        track_rows = (
                            await session.execute(
                                text(
                                    "SELECT id FROM playlist_tracks "
                                    "WHERE playlist_id = :pl_id ORDER BY position"
                                ),
                                {"pl_id": pl_id},
                            )
                        ).fetchall()
                        for pos, (pt_id,) in enumerate(track_rows):
                            await session.execute(
                                text(
                                    "UPDATE playlist_tracks SET position = :pos WHERE id = :pt_id"
                                ),
                                {"pos": pos, "pt_id": pt_id},
                            )

            if execute:
                await session.commit()
                print(
                    f"\n  Applied: {tracks_deleted} tracks deleted, {playlists_remapped} playlist entries remapped."
                )
            else:
                print("\n  Dry run — no changes made. Run with --execute to apply.")

    finally:
        await engine.dispose()

    print(f"\n{'=' * 50}")
    print("Dedup migration complete:")
    print(f"  Groups found:        {groups_found}")
    print(f"  Tracks deleted:      {tracks_deleted}")
    print(f"  Playlists remapped:  {playlists_remapped}")
    print()

    log.info(
        "dedup_migration_completed",
        groups_found=groups_found,
        tracks_deleted=tracks_deleted,
        playlists_remapped=playlists_remapped,
    )


def main() -> None:
    parser = argparse.ArgumentParser(description="Retroactive dedup cleanup")
    parser.add_argument("--execute", action="store_true", help="Apply changes (default: dry-run)")
    args = parser.parse_args()
    asyncio.run(_run(execute=args.execute))


if __name__ == "__main__":
    main()
