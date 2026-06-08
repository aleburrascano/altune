"""GET /v1/tracks end-to-end: happy path, 422 contract, user isolation.

Spins testcontainers Postgres, applies the schema via Base.metadata.create_all
(the alembic migration is verified independently in Slice 4), then drives the
route via FastAPI's sync TestClient context manager so the lifespan runs.
"""

from __future__ import annotations

import asyncio
import os
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import TYPE_CHECKING
from uuid import UUID

import pytest
from alembic import command
from alembic.config import Config
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
    # Post-Slice-4: current_user_id no longer reads settings.hardcoded_user_id;
    # it reads the Authorization header via the TokenVerifier. Use FastAPI's
    # dependency_overrides to substitute the user identity without minting a
    # real JWT (the same seam Slice 7's isolation e2e uses).
    settings = Settings(  # type: ignore[call-arg]
        _env_file=None,
        database_url=url,
        env="test",
        supabase_project_url="https://fixture.supabase.co",
        supabase_jwt_jwks_url="https://fixture.supabase.co/auth/v1/keys",
    )
    app = create_app(settings=settings)
    app.dependency_overrides[current_user_id] = lambda: user
    return TestClient(app)


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


@pytest.fixture(scope="module")
def migrated_db() -> Iterator[str]:
    """Own container migrated via alembic — carries UNIQUE(user_id, dedup_key).

    The POST path uses ON CONFLICT (user_id, dedup_key), which needs the real
    constraint. Base.metadata.create_all (the `fresh_db` route) intentionally
    omits it so the read-path tests can seed several arbitrary rows per user, so
    the write-path test gets an isolated migrated schema instead. env.py prefers
    os.environ["DATABASE_URL"], so the container URL must be pushed there.
    """
    with PostgresContainer("postgres:16-alpine") as container:
        raw = container.get_connection_url()
        async_url = raw.replace("+psycopg2", "+asyncpg").replace("+psycopg", "+asyncpg")
        prior = os.environ.get("DATABASE_URL")
        os.environ["DATABASE_URL"] = async_url
        try:
            root = Path(__file__).resolve().parents[1]
            cfg = Config(str(root.parent / "alembic.ini"))
            cfg.set_main_option("script_location", str(root.parent / "migrations"))
            cfg.set_main_option("sqlalchemy.url", async_url)
            command.upgrade(cfg, "head")
            yield async_url
        finally:
            if prior is None:
                os.environ.pop("DATABASE_URL", None)
            else:
                os.environ["DATABASE_URL"] = prior


@pytest.mark.e2e
def test_post_tracks_creates_201_then_dedupes_200(migrated_db: str) -> None:
    body = {
        "title": "Midnight City",
        "artist": "M83",
        "album": "Hurry Up, We're Dreaming",
        "duration_seconds": 244,
        "artwork_url": "https://img.example/mc.jpg",
    }
    with _client_for(migrated_db, _USER_A) as client:
        first = client.post("/v1/tracks", json=body)
        # Case-insensitive natural key → second POST is a dedup hit, 200 + same id.
        second = client.post("/v1/tracks", json={**body, "title": "midnight city", "artist": "m83"})

    assert first.status_code == 201
    assert second.status_code == 200
    assert first.json()["id"] == second.json()["id"]
    assert first.json()["title"] == "Midnight City"


@pytest.mark.e2e
def test_list_tracks_includes_acquisition_status(fresh_db: str) -> None:
    _seed(fresh_db, [_track(user=_USER_A, id_hex="44444444-4444-4444-4444-444444444444")])
    with _client_for(fresh_db, _USER_A) as client:
        response = client.get("/v1/tracks")
    assert response.status_code == 200
    item = response.json()["items"][0]
    assert item["acquisition_status"] == "pending"
    assert item["artwork_url"] is None


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
