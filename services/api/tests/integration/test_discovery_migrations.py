# mypy: warn_unused_ignores = False
"""discover-music-v1 alembic migrations apply cleanly + create expected schema.

Slices 2 + 3 of the plan. Single test module covers both migrations because
they share a testcontainers Postgres fixture (container startup is the
slow part; running 2 migrations against one container is cheap).

Schema inspection uses raw asyncpg against pg_catalog rather than SQLAlchemy
inspect() — keeps the test dependency surface small (no psycopg2/psycopg
required just for schema queries).
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
def postgres_asyncpg_url() -> Iterator[str]:
    with PostgresContainer("postgres:16-alpine") as container:
        raw = container.get_connection_url()
        asyncpg_url = raw.replace("+psycopg2", "+asyncpg").replace("+psycopg", "+asyncpg")
        yield asyncpg_url


@pytest.fixture(scope="module")
def upgraded_to_head(postgres_asyncpg_url: str) -> Iterator[str]:
    # env.py's load_dotenv + os.environ.get("DATABASE_URL") override
    # config.set_main_option, so we set DATABASE_URL in-process. Restore
    # the prior value on teardown so other test modules aren't surprised.
    prior = os.environ.get("DATABASE_URL")
    os.environ["DATABASE_URL"] = postgres_asyncpg_url
    try:
        config = Config(str(Path(__file__).resolve().parents[2] / "alembic.ini"))
        config.set_main_option("sqlalchemy.url", postgres_asyncpg_url)
        command.upgrade(config, "head")
        yield postgres_asyncpg_url
    finally:
        if prior is None:
            os.environ.pop("DATABASE_URL", None)
        else:
            os.environ["DATABASE_URL"] = prior


def _raw_url(asyncpg_url: str) -> str:
    """Convert SQLAlchemy postgresql+asyncpg URL to a plain asyncpg URL."""
    return asyncpg_url.replace("postgresql+asyncpg://", "postgresql://")


@pytest.mark.integration
@pytest.mark.asyncio
async def test_discovery_search_history_columns(upgraded_to_head: str) -> None:
    conn = await asyncpg.connect(_raw_url(upgraded_to_head))
    try:
        rows = await conn.fetch(
            "SELECT column_name, is_nullable FROM information_schema.columns "
            "WHERE table_name = $1 ORDER BY ordinal_position",
            "discovery_search_history",
        )
    finally:
        await conn.close()
    columns = {row["column_name"]: row["is_nullable"] for row in rows}
    assert set(columns) == {
        "id",
        "user_id",
        "query",
        "query_norm",
        "executed_at",
        "result_clicked_signature",
    }
    assert columns["user_id"] == "NO"
    assert columns["query"] == "NO"
    assert columns["query_norm"] == "NO"
    assert columns["executed_at"] == "NO"
    assert columns["result_clicked_signature"] == "YES"


@pytest.mark.integration
@pytest.mark.asyncio
async def test_discovery_search_history_user_idx(upgraded_to_head: str) -> None:
    conn = await asyncpg.connect(_raw_url(upgraded_to_head))
    try:
        rows = await conn.fetch(
            "SELECT indexname FROM pg_indexes WHERE tablename = $1",
            "discovery_search_history",
        )
    finally:
        await conn.close()
    names = {row["indexname"] for row in rows}
    assert "discovery_search_history_user_idx" in names


@pytest.mark.integration
@pytest.mark.asyncio
async def test_discovery_search_clicks_columns(upgraded_to_head: str) -> None:
    conn = await asyncpg.connect(_raw_url(upgraded_to_head))
    try:
        rows = await conn.fetch(
            "SELECT column_name, is_nullable FROM information_schema.columns "
            "WHERE table_name = $1 ORDER BY ordinal_position",
            "discovery_search_clicks",
        )
    finally:
        await conn.close()
    columns = {row["column_name"]: row["is_nullable"] for row in rows}
    assert set(columns) == {
        "id",
        "user_id",
        "query_norm",
        "result_signature",
        "position",
        "confidence",
        "clicked_at",
    }
    assert columns["user_id"] == "NO"
    assert columns["result_signature"] == "NO"
    assert columns["confidence"] == "NO"


@pytest.mark.integration
@pytest.mark.asyncio
async def test_discovery_search_clicks_confidence_check_constraint(
    upgraded_to_head: str,
) -> None:
    conn = await asyncpg.connect(_raw_url(upgraded_to_head))
    try:
        rows = await conn.fetch(
            "SELECT conname, pg_get_constraintdef(oid) AS def "
            "FROM pg_constraint WHERE conrelid = $1::regclass AND contype = 'c'",
            "discovery_search_clicks",
        )
    finally:
        await conn.close()
    defs = [row["def"] for row in rows]
    assert any(
        "confidence" in d.lower() and "high" in d.lower() and "medium" in d.lower() for d in defs
    ), f"Expected confidence CHECK constraint enumerating high/medium/low; got: {defs}"
