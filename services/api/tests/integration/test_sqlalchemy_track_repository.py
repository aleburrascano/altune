"""SqlAlchemyTrackRepository against a real Postgres.

Uses testcontainers to spin Postgres 16. Schema is materialized via
``Base.metadata.create_all`` rather than running alembic; the alembic
migration is verified independently in Slice 4. Tests use explicit literal
UUIDs so set-equality assertions are decidable per the spec's AC#4
mitigation.
"""

from __future__ import annotations

from datetime import UTC, datetime, timedelta
from typing import TYPE_CHECKING
from uuid import UUID

import pytest
from sqlalchemy import delete
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker, create_async_engine
from testcontainers.postgres import PostgresContainer

from altune.adapters.outbound.persistence.base import Base
from altune.adapters.outbound.persistence.catalog.track_repository import (
    SqlAlchemyTrackRepository,
)
from altune.adapters.outbound.persistence.catalog.track_row import TrackRow
from altune.domain.catalog.track_id import TrackId
from altune.domain.shared.user_id import UserId

if TYPE_CHECKING:
    from collections.abc import AsyncIterator, Iterator

    from sqlalchemy.ext.asyncio import AsyncEngine

    from altune.domain.catalog.track import Track

_USER_A = UserId(UUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))
_USER_B = UserId(UUID("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"))
_BASE_TIME = datetime(2026, 5, 1, 12, 0, tzinfo=UTC)


@pytest.fixture(scope="module")
def postgres_url() -> Iterator[str]:
    with PostgresContainer("postgres:16-alpine") as container:
        raw = container.get_connection_url()
        asyncpg = raw.replace("+psycopg2", "+asyncpg").replace("+psycopg", "+asyncpg")
        yield asyncpg


@pytest.fixture
async def engine(postgres_url: str) -> AsyncIterator[AsyncEngine]:
    # Function-scoped: asyncpg's connection pool binds to the current event
    # loop and pytest-asyncio runs each test in a fresh loop, so a
    # module-scoped engine would carry stale-loop connections into the next
    # test ("another operation is in progress" / "attached to a different
    # loop"). The container above stays module-scoped — that's the slow part.
    eng = create_async_engine(postgres_url)
    async with eng.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)
        await conn.execute(delete(TrackRow))
    yield eng
    await eng.dispose()


@pytest.fixture
async def session(engine: AsyncEngine) -> AsyncIterator[AsyncSession]:
    factory = async_sessionmaker(engine, expire_on_commit=False)
    async with factory() as s:
        yield s


def _track_row(
    *,
    user: UserId,
    id_hex: str,
    title: str = "T",
    added: datetime | None = None,
) -> TrackRow:
    return TrackRow(
        id=UUID(id_hex),
        user_id=user.value,
        title=title,
        artist="A",
        album=None,
        duration_seconds=None,
        added_at=added or _BASE_TIME,
    )


def _to_domain(row: TrackRow) -> Track:
    return row.to_domain()


@pytest.mark.integration
async def test_sqlalchemy_track_repo_returns_only_current_user_rows_in_order(
    session: AsyncSession,
) -> None:
    a_old = _track_row(user=_USER_A, id_hex="11111111-1111-1111-1111-111111111111")
    a_new = _track_row(
        user=_USER_A,
        id_hex="22222222-2222-2222-2222-222222222222",
        added=_BASE_TIME + timedelta(hours=1),
    )
    b_track = _track_row(user=_USER_B, id_hex="33333333-3333-3333-3333-333333333333")
    session.add_all([a_old, a_new, b_track])
    await session.commit()

    repo = SqlAlchemyTrackRepository(session)
    items, total = await repo.list_for_user(_USER_A, limit=50, offset=0)

    assert [t.id for t in items] == [TrackId(a_new.id), TrackId(a_old.id)]
    assert total == 2


@pytest.mark.integration
async def test_sqlalchemy_track_repo_orders_with_id_desc_tiebreaker(
    session: AsyncSession,
) -> None:
    low = _track_row(user=_USER_A, id_hex="aaaaaaaa-1111-1111-1111-111111111111")
    high = _track_row(user=_USER_A, id_hex="ffffffff-1111-1111-1111-111111111111")
    session.add_all([low, high])
    await session.commit()

    repo = SqlAlchemyTrackRepository(session)
    items, _ = await repo.list_for_user(_USER_A, limit=50, offset=0)

    assert [t.id for t in items] == [TrackId(high.id), TrackId(low.id)]


@pytest.mark.integration
async def test_sqlalchemy_track_repo_paginates(session: AsyncSession) -> None:
    rows = [
        _track_row(
            user=_USER_A,
            id_hex=f"00000000-0000-0000-0000-{i:012x}",
            added=_BASE_TIME + timedelta(minutes=i),
        )
        for i in range(7)
    ]
    session.add_all(rows)
    await session.commit()

    repo = SqlAlchemyTrackRepository(session)
    page_1, total = await repo.list_for_user(_USER_A, limit=3, offset=0)
    page_2, _ = await repo.list_for_user(_USER_A, limit=3, offset=3)

    assert len(page_1) == 3
    assert len(page_2) == 3
    assert total == 7
    # Pages don't overlap.
    assert {t.id for t in page_1}.isdisjoint({t.id for t in page_2})


@pytest.mark.integration
async def test_sqlalchemy_track_repo_returns_empty_for_user_with_no_tracks(
    session: AsyncSession,
) -> None:
    session.add(_track_row(user=_USER_B, id_hex="11111111-2222-3333-4444-555555555555"))
    await session.commit()

    repo = SqlAlchemyTrackRepository(session)
    items, total = await repo.list_for_user(_USER_A, limit=50, offset=0)

    assert items == ()
    assert total == 0
