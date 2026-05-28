# mypy: warn_unused_ignores = False, disable_error_code = "no-any-return,untyped-decorator"
"""SqlAlchemySearchHistoryRepository — slice 37 integration tests against real Postgres."""

from __future__ import annotations

from datetime import UTC, datetime, timedelta
from typing import TYPE_CHECKING
from uuid import UUID

import pytest
from sqlalchemy import delete
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker, create_async_engine
from testcontainers.postgres import PostgresContainer

from altune.adapters.outbound.persistence.base import Base
from altune.adapters.outbound.persistence.discovery.search_history_repository import (
    SqlAlchemySearchHistoryRepository,
)
from altune.adapters.outbound.persistence.discovery.search_history_row import (
    SearchHistoryRow,
)
from altune.domain.discovery.search_history_entry import (
    SearchHistoryEntry,
    SearchHistoryEntryId,
)
from altune.domain.shared.user_id import UserId

if TYPE_CHECKING:
    from collections.abc import AsyncIterator, Iterator

    from sqlalchemy.ext.asyncio import AsyncEngine

_USER_A = UserId(UUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))
_USER_B = UserId(UUID("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"))
_BASE_TIME = datetime(2026, 5, 27, 12, 0, tzinfo=UTC)


def _entry(user: UserId, query: str, query_norm: str, offset: int) -> SearchHistoryEntry:
    return SearchHistoryEntry(
        id=SearchHistoryEntryId(UUID(int=offset + 1)),
        user_id=user,
        query=query,
        query_norm=query_norm,
        executed_at=_BASE_TIME + timedelta(minutes=offset),
        result_clicked_signature=None,
    )


@pytest.fixture(scope="module")
def postgres_url() -> Iterator[str]:
    with PostgresContainer("postgres:16-alpine") as container:
        raw = container.get_connection_url()
        asyncpg = raw.replace("+psycopg2", "+asyncpg").replace("+psycopg", "+asyncpg")
        yield asyncpg


@pytest.fixture
async def engine(postgres_url: str) -> AsyncIterator[AsyncEngine]:
    eng = create_async_engine(postgres_url)
    async with eng.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)
        await conn.execute(delete(SearchHistoryRow))
    yield eng
    await eng.dispose()


@pytest.fixture
def sessionmaker(engine: AsyncEngine) -> async_sessionmaker[AsyncSession]:
    return async_sessionmaker(engine, expire_on_commit=False)


@pytest.mark.integration
@pytest.mark.asyncio
async def test_history_repo_inserts_and_lists(
    sessionmaker: async_sessionmaker[AsyncSession],
) -> None:
    async with sessionmaker() as session:
        repo = SqlAlchemySearchHistoryRepository(session)
        await repo.insert(_entry(_USER_A, "Beatles", "beatles", 1))
        await repo.insert(_entry(_USER_A, "Stones", "stones", 2))
        await session.commit()

        listed = await repo.list_distinct_recent(_USER_A, limit=10)
    assert len(listed) == 2
    assert listed[0].query_norm == "stones"  # newest first
    assert listed[1].query_norm == "beatles"


@pytest.mark.integration
@pytest.mark.asyncio
async def test_history_repo_trims_to_n_keeps_latest_only(
    sessionmaker: async_sessionmaker[AsyncSession],
) -> None:
    async with sessionmaker() as session:
        repo = SqlAlchemySearchHistoryRepository(session)
        for i in range(60):
            await repo.insert(_entry(_USER_A, f"q{i}", f"qn{i}", offset=i))
        await session.commit()
        await repo.trim_to_n(_USER_A, n=50)
        await session.commit()

        listed = await repo.list_distinct_recent(_USER_A, limit=100)
    assert len(listed) == 50
    # The 50 newest should be qn10..qn59 (since 0..59 inserted with monotonic offset).
    norms = {e.query_norm for e in listed}
    assert "qn59" in norms
    assert "qn0" not in norms


@pytest.mark.integration
@pytest.mark.asyncio
async def test_history_repo_lists_distinct_by_query_norm(
    sessionmaker: async_sessionmaker[AsyncSession],
) -> None:
    async with sessionmaker() as session:
        repo = SqlAlchemySearchHistoryRepository(session)
        # Same user searches "beatles" three times — only the latest shows.
        await repo.insert(_entry(_USER_A, "Beatles", "beatles", 1))
        await repo.insert(_entry(_USER_A, "BEATLES", "beatles", 2))
        await repo.insert(_entry(_USER_A, "Beatles?", "beatles", 3))
        await repo.insert(_entry(_USER_A, "Stones", "stones", 4))
        await session.commit()

        listed = await repo.list_distinct_recent(_USER_A, limit=10)
    norms = [e.query_norm for e in listed]
    assert norms == ["stones", "beatles"]
    # The "beatles" entry returned should be the most recent (offset 3, query="Beatles?").
    beatles_entry = next(e for e in listed if e.query_norm == "beatles")
    assert beatles_entry.query == "Beatles?"


@pytest.mark.integration
@pytest.mark.asyncio
async def test_history_repo_isolates_users(
    sessionmaker: async_sessionmaker[AsyncSession],
) -> None:
    async with sessionmaker() as session:
        repo = SqlAlchemySearchHistoryRepository(session)
        await repo.insert(_entry(_USER_A, "Alpha", "alpha", 1))
        await repo.insert(_entry(_USER_B, "Beta", "beta", 2))
        await session.commit()

        a_list = await repo.list_distinct_recent(_USER_A, limit=10)
        b_list = await repo.list_distinct_recent(_USER_B, limit=10)
    assert {e.query_norm for e in a_list} == {"alpha"}
    assert {e.query_norm for e in b_list} == {"beta"}


@pytest.mark.integration
@pytest.mark.asyncio
async def test_history_repo_returns_empty_for_user_with_no_rows(
    sessionmaker: async_sessionmaker[AsyncSession],
) -> None:
    async with sessionmaker() as session:
        repo = SqlAlchemySearchHistoryRepository(session)
        listed = await repo.list_distinct_recent(_USER_A, limit=10)
    assert listed == ()
