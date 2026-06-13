# ruff: noqa: T201
"""Fix audio_ref paths — strip the user-UUID prefix so refs are relative.

Before: 955fca87-3a19-415f-b9b8-c9b934b39524/SLAYR/Half Blood (BloodLuxe)/Toxic.mp3
After:  SLAYR/Half Blood (BloodLuxe)/Toxic.mp3

Usage:
    uv run python -m altune.adapters.inbound.cli.fix_audio_refs              # dry-run
    uv run python -m altune.adapters.inbound.cli.fix_audio_refs --execute    # apply
"""

from __future__ import annotations

import argparse
import asyncio
import re
import sys

UUID_PREFIX_RE = re.compile(r"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/")


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
    fixed = 0

    try:
        async with sessionmaker() as session:
            rows = (
                await session.execute(
                    text("SELECT id, audio_ref FROM tracks WHERE audio_ref IS NOT NULL")
                )
            ).fetchall()

            print(f"\nChecking {len(rows)} tracks with audio_ref...\n")

            for track_id, audio_ref in rows:
                if UUID_PREFIX_RE.match(audio_ref):
                    new_ref = UUID_PREFIX_RE.sub("", audio_ref)
                    print(f"  {audio_ref}")
                    print(f"  -> {new_ref}")
                    print()
                    if execute:
                        await session.execute(
                            text("UPDATE tracks SET audio_ref = :new WHERE id = :id"),
                            {"new": new_ref, "id": track_id},
                        )
                    fixed += 1

            if execute and fixed > 0:
                await session.commit()

    finally:
        await engine.dispose()

    print(f"{'='*50}")
    print(f"Audio ref fix: {fixed} refs {'updated' if execute else 'would be updated'}")
    if not execute and fixed > 0:
        print("Run with --execute to apply.")
    print()


def main() -> None:
    parser = argparse.ArgumentParser(description="Strip UUID prefix from audio_ref paths")
    parser.add_argument("--execute", action="store_true", help="Apply changes (default: dry-run)")
    args = parser.parse_args()
    asyncio.run(_run(execute=args.execute))


if __name__ == "__main__":
    main()
