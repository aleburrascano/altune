"""Port-contract conformance — `InMemoryTrackRepository` vs `SqlAlchemyTrackRepository`.

Both implement `application.catalog.ports.TrackRepository`. This module runs
the same scenarios against both so behavior can't drift between unit tests
(fake) and integration tests (real DB). The risk this guards against is
called out in `docs/specs/view-library/plan.md` Risks.

The SqlAlchemy parametrization needs Docker (testcontainers Postgres). The
in-memory parametrization doesn't. Both run under the single ``contract``
marker so ``uv run pytest -m contract`` is honest about pass-rate.
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
from altune.domain.catalog.track import Track
from altune.domain.catalog.track_id import TrackId
from altune.domain.shared.user_id import UserId
from tests._doubles.in_memory_track_repository import InMemoryTrackRepository

if TYPE_CHECKING:
    from collections.abc import AsyncIterator, Awaitable, Callable, Iterator

    from altune.application.catalog.ports import TrackRepository

    SeedRepo = Callable[[list[Track]], Awaitable[TrackRepository]]

_USER_A = UserId(UUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))
_USER_B = UserId(UUID("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"))
_BASE_TIME = datetime(2026, 5, 1, 12, 0, tzinfo=UTC)


def _track(
    *,
    user: UserId,
    id_hex: str,
    added: datetime | None = None,
    title: str = "T",
) -> Track:
    return Track(
        id=TrackId(UUID(id_hex)),
        user_id=user,
        title=title,
        artist="A",
        album=None,
        duration_seconds=None,
        added_at=added or _BASE_TIME,
    )


@pytest.fixture(scope="module")
def postgres_url() -> Iterator[str]:
    with PostgresContainer("postgres:16-alpine") as container:
        raw = container.get_connection_url()
        yield raw.replace("+psycopg2", "+asyncpg").replace("+psycopg", "+asyncpg")


@pytest.fixture
async def sqlalchemy_session(postgres_url: str) -> AsyncIterator[AsyncSession]:
    eng = create_async_engine(postgres_url)
    async with eng.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)
        await conn.execute(delete(TrackRow))
    factory = async_sessionmaker(eng, expire_on_commit=False)
    async with factory() as session:
        yield session
    await eng.dispose()


@pytest.fixture(params=["in_memory", "sqlalchemy"])
async def seed_repo(request: pytest.FixtureRequest, sqlalchemy_session: AsyncSession) -> SeedRepo:
    """Returns an async callable that seeds the given tracks and yields a configured repo."""
    if request.param == "in_memory":

        async def setup_in_memory(tracks: list[Track]) -> TrackRepository:
            return InMemoryTrackRepository(tracks)

        return setup_in_memory

    async def setup_sqlalchemy(tracks: list[Track]) -> TrackRepository:
        sqlalchemy_session.add_all([TrackRow.from_domain(t) for t in tracks])
        await sqlalchemy_session.commit()
        return SqlAlchemyTrackRepository(sqlalchemy_session)

    return setup_sqlalchemy


@pytest.mark.contract
async def test_both_repositories_return_same_ordering_for_same_seed(
    seed_repo: SeedRepo,
) -> None:
    tracks = [
        _track(user=_USER_A, id_hex="11111111-1111-1111-1111-111111111111", added=_BASE_TIME),
        _track(
            user=_USER_A,
            id_hex="22222222-2222-2222-2222-222222222222",
            added=_BASE_TIME + timedelta(hours=1),
        ),
        _track(user=_USER_A, id_hex="33333333-3333-3333-3333-333333333333", added=_BASE_TIME),
    ]
    repo = await seed_repo(tracks)

    items, total = await repo.list_for_user(_USER_A, limit=50, offset=0)

    # Expected: hour-later first (newest by added_at), then id DESC on the ties.
    assert [t.id for t in items] == [
        TrackId(UUID("22222222-2222-2222-2222-222222222222")),
        TrackId(UUID("33333333-3333-3333-3333-333333333333")),
        TrackId(UUID("11111111-1111-1111-1111-111111111111")),
    ]
    assert total == 3


@pytest.mark.contract
async def test_both_repositories_isolate_users(seed_repo: SeedRepo) -> None:
    tracks = [
        _track(user=_USER_A, id_hex="aaaaaaaa-aaaa-aaaa-aaaa-111111111111"),
        _track(user=_USER_B, id_hex="bbbbbbbb-bbbb-bbbb-bbbb-222222222222"),
    ]
    repo = await seed_repo(tracks)

    items_a, total_a = await repo.list_for_user(_USER_A, limit=50, offset=0)
    items_b, total_b = await repo.list_for_user(_USER_B, limit=50, offset=0)

    assert [t.id for t in items_a] == [TrackId(UUID("aaaaaaaa-aaaa-aaaa-aaaa-111111111111"))]
    assert [t.id for t in items_b] == [TrackId(UUID("bbbbbbbb-bbbb-bbbb-bbbb-222222222222"))]
    assert total_a == 1
    assert total_b == 1


@pytest.mark.contract
async def test_both_repositories_paginate_and_report_total(
    seed_repo: SeedRepo,
) -> None:
    tracks = [
        _track(
            user=_USER_A,
            id_hex=f"00000000-0000-0000-0000-{i:012x}",
            added=_BASE_TIME + timedelta(minutes=i),
        )
        for i in range(5)
    ]
    repo = await seed_repo(tracks)

    page_1, total = await repo.list_for_user(_USER_A, limit=2, offset=0)
    page_2, _ = await repo.list_for_user(_USER_A, limit=2, offset=2)

    assert len(page_1) == 2
    assert len(page_2) == 2
    assert total == 5
    assert {t.id for t in page_1}.isdisjoint({t.id for t in page_2})


@pytest.mark.contract
async def test_both_repositories_return_empty_for_user_with_no_tracks(
    seed_repo: SeedRepo,
) -> None:
    repo = await seed_repo([_track(user=_USER_B, id_hex="11111111-2222-3333-4444-555555555555")])

    items, total = await repo.list_for_user(_USER_A, limit=50, offset=0)

    assert items == ()
    assert total == 0
