# mypy: warn_unused_ignores = False
"""view-result-detail catalog migration applies cleanly + creates expected schema.

Spins a throwaway Postgres via testcontainers, runs `alembic upgrade head`, and
asserts the `tracks` table gained the acquisition/dedup columns and the
UNIQUE(user_id, dedup_key) constraint behind spec AC#7.

Mirrors tests/integration/test_discovery_migrations.py: alembic's env.py prefers
os.environ["DATABASE_URL"] (it calls load_dotenv at import), so the container URL
MUST be pushed there — config.set_main_option alone is silently overridden by a
stray .env, which makes the upgrade run against the wrong DB. Schema inspection
uses raw asyncpg (no psycopg2 in this project).
"""

from __future__ import annotations

import os
from pathlib import Path
from typing import TYPE_CHECKING

import asyncpg
import pytest
from alembic import command  # type: ignore[import-not-found]
from alembic.config import Config  # type: ignore[import-not-found]
from testcontainers.postgres import PostgresContainer  # type: ignore[import-not-found]

if TYPE_CHECKING:
    from collections.abc import Iterator


@pytest.fixture(scope="module")
def upgraded_to_head() -> Iterator[str]:
    with PostgresContainer("postgres:16-alpine") as container:
        raw = container.get_connection_url()
        asyncpg_url = raw.replace("+psycopg2", "+asyncpg").replace("+psycopg", "+asyncpg")
        prior = os.environ.get("DATABASE_URL")
        os.environ["DATABASE_URL"] = asyncpg_url
        try:
            config = Config(str(Path(__file__).resolve().parents[2] / "alembic.ini"))
            config.set_main_option("sqlalchemy.url", asyncpg_url)
            command.upgrade(config, "head")
            yield asyncpg_url
        finally:
            if prior is None:
                os.environ.pop("DATABASE_URL", None)
            else:
                os.environ["DATABASE_URL"] = prior


def _raw_url(asyncpg_url: str) -> str:
    return asyncpg_url.replace("postgresql+asyncpg://", "postgresql://")


@pytest.mark.integration
@pytest.mark.asyncio
async def test_catalog_migration_adds_acquisition_columns(upgraded_to_head: str) -> None:
    conn = await asyncpg.connect(_raw_url(upgraded_to_head))
    try:
        rows = await conn.fetch(
            "SELECT column_name FROM information_schema.columns WHERE table_name = $1",
            "tracks",
        )
    finally:
        await conn.close()
    columns = {row["column_name"] for row in rows}
    assert {"artwork_url", "acquisition_status", "dedup_key"}.issubset(columns)


@pytest.mark.integration
@pytest.mark.asyncio
async def test_catalog_migration_adds_unique_dedup_constraint(upgraded_to_head: str) -> None:
    conn = await asyncpg.connect(_raw_url(upgraded_to_head))
    try:
        rows = await conn.fetch(
            "SELECT conname FROM pg_constraint WHERE conrelid = $1::regclass AND contype = 'u'",
            "tracks",
        )
    finally:
        await conn.close()
    names = {row["conname"] for row in rows}
    assert "uq_tracks_user_dedup" in names
