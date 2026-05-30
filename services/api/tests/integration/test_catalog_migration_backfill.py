"""The dedup migration backfills existing rows before the unique index.

Regression for a real failure: applying a1c4e7b9d2f3 to a NON-empty `tracks`
table (e.g. a dev DB with seeded rows) raised
``could not create unique index "uq_tracks_user_dedup"`` because every existing
row got the temporary ``dedup_key = ''`` default. The migration now backfills
dedup_key from title/artist/album (matching the domain normalizer) before
adding the constraint, so distinct tracks keep distinct keys.

Spins a throwaway Postgres, upgrades to the PRE-dedup revision, seeds two
distinct tracks for one user, then upgrades to head and asserts the index built
and the keys were backfilled. The test stays SYNC because alembic's env.py runs
``asyncio.run`` internally — driving ``command.upgrade`` from inside a running
loop would raise. asyncpg work is wrapped in ``asyncio.run``. env.py prefers
os.environ["DATABASE_URL"], so the container URL is pushed there.
"""

from __future__ import annotations

import asyncio
import os
from pathlib import Path
from typing import TYPE_CHECKING

import asyncpg
import pytest
from alembic import command
from alembic.config import Config
from testcontainers.postgres import PostgresContainer

if TYPE_CHECKING:
    from collections.abc import Iterator

_PRE_DEDUP_REVISION = "e2bcd72a93f1"
_USER = "00000000-0000-0000-0000-0000000000aa"


@pytest.fixture(scope="module")
def _async_url() -> Iterator[str]:
    with PostgresContainer("postgres:16-alpine") as container:
        raw = container.get_connection_url()
        yield raw.replace("+psycopg2", "+asyncpg").replace("+psycopg", "+asyncpg")


def _raw_url(async_url: str) -> str:
    return async_url.replace("postgresql+asyncpg://", "postgresql://")


async def _seed_two_distinct_tracks(async_url: str) -> None:
    conn = await asyncpg.connect(_raw_url(async_url))
    try:
        await conn.executemany(
            "INSERT INTO tracks (user_id, title, artist, album) VALUES ($1::uuid, $2, $3, $4)",
            [
                (_USER, "Blinding Lights", "The Weeknd", "After Hours"),
                (_USER, "Bohemian Rhapsody", "Queen", "A Night at the Opera"),
            ],
        )
    finally:
        await conn.close()


async def _read_dedup_keys(async_url: str) -> list[str]:
    conn = await asyncpg.connect(_raw_url(async_url))
    try:
        rows = await conn.fetch("SELECT dedup_key FROM tracks WHERE user_id = $1::uuid", _USER)
        constraint = await conn.fetchval(
            "SELECT count(*) FROM pg_constraint WHERE conname = 'uq_tracks_user_dedup'"
        )
    finally:
        await conn.close()
    assert constraint == 1
    return sorted(row["dedup_key"] for row in rows)


@pytest.mark.integration
def test_migration_backfills_dedup_key_on_nonempty_table(_async_url: str) -> None:
    prior = os.environ.get("DATABASE_URL")
    os.environ["DATABASE_URL"] = _async_url
    try:
        config = Config(str(Path(__file__).resolve().parents[2] / "alembic.ini"))
        config.set_main_option("sqlalchemy.url", _async_url)
        command.upgrade(config, _PRE_DEDUP_REVISION)
        asyncio.run(_seed_two_distinct_tracks(_async_url))
        # Would have raised UniqueViolationError before the backfill was added.
        command.upgrade(config, "head")
    finally:
        if prior is None:
            os.environ.pop("DATABASE_URL", None)
        else:
            os.environ["DATABASE_URL"] = prior

    values = asyncio.run(_read_dedup_keys(_async_url))
    assert values == [
        "blinding lights\x1fthe weeknd\x1fafter hours",
        "bohemian rhapsody\x1fqueen\x1fa night at the opera",
    ]
