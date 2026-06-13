# ruff: noqa: T201
"""Storage health-check CLI — detect and optionally fix data inconsistencies.

Usage:
    uv run python -m altune.adapters.inbound.cli.health_check          # report only
    uv run python -m altune.adapters.inbound.cli.health_check --fix    # fix orphaned DB rows
"""

from __future__ import annotations

import argparse
import asyncio
import sys
from pathlib import Path

import structlog

log = structlog.get_logger(__name__)


async def _run(fix: bool) -> None:
    from altune.adapters.outbound.audio.filesystem_store import FilesystemAudioStore
    from altune.adapters.outbound.persistence.catalog.track_repository import (
        SqlAlchemyTrackRepository,
    )
    from altune.application.catalog.reconcile_track_status import (
        ReconcileInput,
        ReconcileTrackStatus,
    )
    from altune.platform.config import Settings
    from altune.platform.db import create_engine, create_sessionmaker

    cfg = Settings()  # type: ignore[call-arg]
    if cfg.database_url is None:
        print("ERROR: DATABASE_URL not set")
        sys.exit(1)
    if cfg.music_dir is None:
        print("ERROR: MUSIC_DIR not set")
        sys.exit(1)

    engine = create_engine(str(cfg.database_url))
    sessionmaker = create_sessionmaker(engine)
    audio_store = FilesystemAudioStore(cfg.music_dir)

    orphaned_db = 0
    fixed = 0
    orphaned_files = 0
    total_checked = 0

    try:
        async with sessionmaker() as session:
            repo = SqlAlchemyTrackRepository(session)
            from sqlalchemy import text

            rows = (
                await session.execute(
                    text(
                        "SELECT id, user_id, title, artist, audio_ref, acquisition_status "
                        "FROM tracks WHERE acquisition_status = 'ready' AND audio_ref IS NOT NULL"
                    )
                )
            ).fetchall()

            total_checked = len(rows)
            print(f"\nChecking {total_checked} tracks with status=ready...\n")

            for row in rows:
                track_id_str, user_id_str, title, artist, audio_ref, _ = row
                if not audio_store.exists(audio_ref):
                    orphaned_db += 1
                    print(f"  ORPHANED: {title} — {artist}  (id={track_id_str}, ref={audio_ref})")

                    if fix:
                        from uuid import UUID

                        from altune.domain.catalog.track_id import TrackId
                        from altune.domain.shared.user_id import UserId

                        use_case = ReconcileTrackStatus(repo, audio_store)
                        result = await use_case.execute(
                            ReconcileInput(
                                track_id=TrackId(UUID(str(track_id_str))),
                                user_id=UserId(UUID(str(user_id_str))),
                                reason="Audio file missing from storage (health-check)",
                            ),
                        )
                        if result.reconciled:
                            fixed += 1
                            print("    → FIXED: marked as failed")

            if fix and fixed > 0:
                await session.commit()

        # Check for orphaned files
        music_path = Path(cfg.music_dir)
        if music_path.is_dir():
            async with sessionmaker() as session:
                all_refs_result = (
                    await session.execute(
                        text("SELECT audio_ref FROM tracks WHERE audio_ref IS NOT NULL")
                    )
                ).fetchall()
                known_refs = {r[0] for r in all_refs_result}

            for file_path in music_path.rglob("*"):
                if file_path.is_file():
                    rel = str(file_path.relative_to(music_path)).replace("\\", "/")
                    if rel not in known_refs:
                        orphaned_files += 1
                        print(f"  ORPHANED FILE: {rel}  (not referenced by any track)")

    finally:
        await engine.dispose()

    print(f"\n{'=' * 50}")
    print("Health check complete:")
    print(f"  Tracks checked:    {total_checked}")
    print(f"  Orphaned DB rows:  {orphaned_db}")
    print(f"  Orphaned files:    {orphaned_files}")
    if fix:
        print(f"  Fixed:             {fixed}")
    elif orphaned_db > 0:
        print("\n  Run with --fix to mark orphaned tracks as failed.")
    print()

    log.info(
        "health_check_completed",
        total_checked=total_checked,
        orphaned_db=orphaned_db,
        orphaned_files=orphaned_files,
        fixed=fixed,
    )


def main() -> None:
    parser = argparse.ArgumentParser(description="Storage health check")
    parser.add_argument("--fix", action="store_true", help="Fix orphaned DB rows (mark as failed)")
    args = parser.parse_args()
    asyncio.run(_run(fix=args.fix))


if __name__ == "__main__":
    main()
