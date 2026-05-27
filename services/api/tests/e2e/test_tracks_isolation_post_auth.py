# mypy: ignore_errors = True
"""Per-user data isolation under the post-Slice-4 auth seam (Slice 7, AC#8).

The complementary half of AC#8: view-library already ships the SQL-level
WHERE-clause isolation test (test_get_tracks_isolates_users). This slice
proves the JWT-derived user_id flows correctly into that clause by using
app.dependency_overrides to feed UserId(A) or UserId(B) without a real
Supabase round-trip.

The dependency-override pattern is the seam ADR-0004 created and Slice 4
preserved: tests substitute the implementation of current_user_id while
the route's signature stays identical.
"""

from __future__ import annotations

import asyncio
from datetime import UTC, datetime
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
from altune.platform.auth import current_user_id
from altune.platform.config import Settings

if TYPE_CHECKING:
    from collections.abc import Iterator

_USER_A = UserId(UUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))
_USER_B = UserId(UUID("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"))
_TIME = datetime(2026, 5, 1, 12, 0, tzinfo=UTC)


def _track(*, user: UserId, id_hex: str) -> Track:
    return Track(
        id=TrackId(UUID(id_hex)),
        user_id=user,
        title="T",
        artist="A",
        album=None,
        duration_seconds=None,
        added_at=_TIME,
    )


@pytest.fixture(scope="module")
def postgres_url() -> Iterator[str]:
    with PostgresContainer("postgres:16-alpine") as container:
        raw = container.get_connection_url()
        yield raw.replace("+psycopg2", "+asyncpg").replace("+psycopg", "+asyncpg")


@pytest.fixture
def fresh_db_with_two_users(postgres_url: str) -> Iterator[str]:
    """Apply schema, truncate, seed A's and B's rows."""

    async def _setup() -> None:
        eng = create_async_engine(postgres_url)
        async with eng.begin() as conn:
            await conn.run_sync(Base.metadata.create_all)
            await conn.execute(delete(TrackRow))
        factory = async_sessionmaker(eng, expire_on_commit=False)
        async with factory() as s:
            s.add_all(
                [
                    TrackRow.from_domain(
                        _track(user=_USER_A, id_hex="aaaaaaaa-1111-1111-1111-111111111111")
                    ),
                    TrackRow.from_domain(
                        _track(user=_USER_A, id_hex="aaaaaaaa-2222-2222-2222-222222222222")
                    ),
                    TrackRow.from_domain(
                        _track(user=_USER_B, id_hex="bbbbbbbb-1111-1111-1111-111111111111")
                    ),
                ]
            )
            await s.commit()
        await eng.dispose()

    asyncio.run(_setup())
    yield postgres_url


def _app_with_user(url: str, user: UserId):
    settings = Settings(
        _env_file=None,
        env="test",
        database_url=url,
        supabase_project_url="https://fixture.supabase.co",
        supabase_jwt_jwks_url="https://fixture.supabase.co/auth/v1/keys",
    )
    app = create_app(settings=settings)
    app.dependency_overrides[current_user_id] = lambda: user
    return app


@pytest.mark.e2e
def test_user_a_sees_only_a_tracks_after_auth_swap(fresh_db_with_two_users: str) -> None:
    app = _app_with_user(fresh_db_with_two_users, _USER_A)
    with TestClient(app) as client:
        response = client.get("/v1/tracks")
    assert response.status_code == 200
    ids = {item["id"] for item in response.json()["items"]}
    assert ids == {
        "aaaaaaaa-1111-1111-1111-111111111111",
        "aaaaaaaa-2222-2222-2222-222222222222",
    }
    assert "bbbbbbbb-1111-1111-1111-111111111111" not in ids


@pytest.mark.e2e
def test_user_b_sees_only_b_tracks_after_auth_swap(fresh_db_with_two_users: str) -> None:
    app = _app_with_user(fresh_db_with_two_users, _USER_B)
    with TestClient(app) as client:
        response = client.get("/v1/tracks")
    assert response.status_code == 200
    ids = {item["id"] for item in response.json()["items"]}
    assert ids == {"bbbbbbbb-1111-1111-1111-111111111111"}
