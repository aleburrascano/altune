#!/usr/bin/env python
# ruff: noqa: T201  -- CLI tool; print is the intended output channel.
"""Snapshot the old Supabase `songs` table to a local JSON fixture.

The harness needs labeled ground truth (real artist + title + album). The
user's old Supabase project holds 1.6k real tracks. We pull them ONCE and
write track metadata to data/library.json so the corpus is deterministic and
offline thereafter.

Credentials come from env vars — NEVER hard-coded, NEVER written to the
snapshot:

    ALTUNE_EVAL_SUPABASE_URL   e.g. https://<ref>.supabase.co
    ALTUNE_EVAL_SUPABASE_KEY   a Supabase API key (service_role or anon)

Usage:
    ALTUNE_EVAL_SUPABASE_URL=... ALTUNE_EVAL_SUPABASE_KEY=... \
        uv run python -m scripts.discovery_eval.snapshot_library
"""

from __future__ import annotations

import json
import os
import sys
from pathlib import Path

import httpx

# Columns we keep — enough to build queries and labels for every kind.
_COLUMNS = ("title", "artist", "album", "album_artist", "year", "genre", "duration")
_PAGE = 1000
_DATA_DIR = Path(__file__).parent / "data"
_OUT = _DATA_DIR / "library.json"


def _fetch_all(base_url: str, key: str) -> list[dict[str, object]]:
    select = ",".join(_COLUMNS)
    rows: list[dict[str, object]] = []
    offset = 0
    with httpx.Client(timeout=30.0) as client:
        while True:
            resp = client.get(
                f"{base_url.rstrip('/')}/rest/v1/songs",
                params={"select": select, "order": "id", "limit": _PAGE, "offset": offset},
                headers={"apikey": key, "Authorization": f"Bearer {key}"},
            )
            resp.raise_for_status()
            page = resp.json()
            rows.extend(page)
            print(f"  fetched {len(rows)} rows...")
            if len(page) < _PAGE:
                break
            offset += _PAGE
    return rows


def main() -> int:
    base_url = os.environ.get("ALTUNE_EVAL_SUPABASE_URL")
    key = os.environ.get("ALTUNE_EVAL_SUPABASE_KEY")
    if not base_url or not key:
        print(
            "ERROR: set ALTUNE_EVAL_SUPABASE_URL and ALTUNE_EVAL_SUPABASE_KEY env vars.",
            file=sys.stderr,
        )
        return 2

    print(f"Snapshotting songs from {base_url} ...")
    rows = _fetch_all(base_url, key)

    # Drop rows missing the load-bearing fields; normalize types lightly.
    clean = [
        {
            "title": str(r["title"]).strip(),
            "artist": str(r["artist"]).strip(),
            "album": (str(r["album"]).strip() if r.get("album") else None),
            "album_artist": (str(r["album_artist"]).strip() if r.get("album_artist") else None),
            "year": r.get("year"),
            "genre": (str(r["genre"]).strip() if r.get("genre") else None),
        }
        for r in rows
        if r.get("title") and r.get("artist")
    ]

    _DATA_DIR.mkdir(parents=True, exist_ok=True)
    _OUT.write_text(json.dumps(clean, ensure_ascii=False, indent=1), encoding="utf-8")
    print(f"Wrote {len(clean)} tracks to {_OUT} (dropped {len(rows) - len(clean)} incomplete).")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
