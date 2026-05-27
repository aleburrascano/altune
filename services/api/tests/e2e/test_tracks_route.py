"""GET /v1/tracks end-to-end: happy path, 422 contract, user isolation.

Spins testcontainers Postgres, applies the schema via Base.metadata.create_all
(the alembic migration is verified independently in Slice 4), then drives the
route via FastAPI's sync TestClient context manager so the lifespan runs.
"""

from __future__ import annotations

import asyncio
from datetime import UTC, datetime, timedelta
from typing import TYPE_CHECKING
from uuid import UUID

import pytest
from fastapi.testclient import TestClient
from sqlalchemy import delete
from sqlalchemy.ext.asyncio import async_sessionmaker, create_async_engine
from testcontainers.postgres import PostgresContainer

from altune.adapters.outbound.persistence.base import Base
from altune.adapters.outbound.persistence.catalog.track_row import TrackRow
from altune.domain.catalog.track import Track
from altune.domain.catalog.track_id import TrackId
from altune.domain.shared.user_id import UserId
from altune.platform.app import create_app
from altune.platform.config import Settings

if TYPE_CHECKING:
    from collections.abc import Iterator

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
def fresh_db(postgres_url: str) -> Iterator[str]:
    """Apply schema + truncate; yield URL for use by the app."""

    async def _setup() -> None:
        eng = create_async_engine(postgres_url)
        async with eng.begin() as conn:
            await conn.run_sync(Base.metadata.create_all)
            await conn.execute(delete(TrackRow))
        await eng.dispose()

    asyncio.run(_setup())
    yield postgres_url


def _seed(url: str, tracks: list[Track]) -> None:
    """Synchronous-looking seed for use inside non-async test bodies."""

    async def _do() -> None:
        eng = create_async_engine(url)
        factory = async_sessionmaker(eng, expire_on_commit=False)
        async with factory() as s:
            s.add_all([TrackRow.from_domain(t) for t in tracks])
            await s.commit()
        await eng.dispose()

    asyncio.run(_do())


def _client_for(url: str, user: UserId) -> TestClient:
    settings = Settings(  # type: ignore[call-arg]
        _env_file=None,
        database_url=url,
        hardcoded_user_id=user.value,
        env="test",
    )
    return TestClient(create_app(settings=settings))


@pytest.mark.e2e
def test_get_tracks_returns_paginated_response_for_current_user(fresh_db: str) -> None:
    _seed(
        fresh_db,
        [
            _track(user=_USER_A, id_hex="11111111-1111-1111-1111-111111111111", added=_BASE_TIME),
            _track(
                user=_USER_A,
                id_hex="22222222-2222-2222-2222-222222222222",
                added=_BASE_TIME + timedelta(hours=1),
            ),
            _track(user=_USER_A, id_hex="33333333-3333-3333-3333-333333333333", added=_BASE_TIME),
        ],
    )
    with _client_for(fresh_db, _USER_A) as client:
        response = client.get("/v1/tracks?limit=50&offset=0")
    assert response.status_code == 200
    body = response.json()
    assert body["total"] == 3
    assert body["limit"] == 50
    assert body["offset"] == 0
    assert body["has_more"] is False
    # Order: hour-later first, then id DESC on the ties.
    assert [item["id"] for item in body["items"]] == [
        "22222222-2222-2222-2222-222222222222",
        "33333333-3333-3333-3333-333333333333",
        "11111111-1111-1111-1111-111111111111",
    ]


@pytest.mark.e2e
@pytest.mark.parametrize(
    "query",
    ["?limit=0", "?limit=201", "?offset=-1"],
)
def test_get_tracks_422_for_out_of_range_query(fresh_db: str, query: str) -> None:
    with _client_for(fresh_db, _USER_A) as client:
        response = client.get(f"/v1/tracks{query}")
    assert response.status_code == 422


@pytest.mark.e2e
def test_get_tracks_isolates_users(fresh_db: str) -> None:
    _seed(
        fresh_db,
        [
            _track(user=_USER_A, id_hex="aaaaaaaa-1111-1111-1111-111111111111"),
            _track(user=_USER_A, id_hex="aaaaaaaa-2222-2222-2222-222222222222"),
            _track(user=_USER_B, id_hex="bbbbbbbb-1111-1111-1111-111111111111"),
        ],
    )
    with _client_for(fresh_db, _USER_A) as client:
        response = client.get("/v1/tracks")
    assert response.status_code == 200
    ids = {item["id"] for item in response.json()["items"]}
    assert "bbbbbbbb-1111-1111-1111-111111111111" not in ids
    assert ids == {
        "aaaaaaaa-1111-1111-1111-111111111111",
        "aaaaaaaa-2222-2222-2222-222222222222",
    }
