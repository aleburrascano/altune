"""GET /v1/tracks/{track_id}/audio — audio streaming e2e tests.

Spins testcontainers Postgres, seeds a READY track with an audio file on
disk, and verifies the streaming endpoint returns correct headers and content.
"""

from __future__ import annotations

import asyncio
from datetime import UTC, datetime
from typing import TYPE_CHECKING
from uuid import UUID

import pytest
from fastapi.testclient import TestClient
from sqlalchemy.ext.asyncio import async_sessionmaker, create_async_engine
from testcontainers.postgres import PostgresContainer

from altune.adapters.outbound.persistence.base import Base
from altune.adapters.outbound.persistence.catalog.track_row import TrackRow
from altune.domain.catalog.acquisition_status import AcquisitionStatus
from altune.domain.catalog.track import Track
from altune.domain.catalog.track_id import TrackId
from altune.domain.shared.user_id import UserId
from altune.platform.app import create_app
from altune.platform.auth import current_user_id
from altune.platform.config import Settings

if TYPE_CHECKING:
    from collections.abc import Iterator
    from pathlib import Path

_USER = UserId(UUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))
_TRACK_ID = "11111111-1111-1111-1111-111111111111"
_AUDIO_REF = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa/Artist/Album/Track.mp3"
_FAKE_MP3 = b"\xff\xfb\x90\x00" + b"\x00" * 1000


def _ready_track() -> Track:
    return Track(
        id=TrackId(UUID(_TRACK_ID)),
        user_id=_USER,
        title="Track",
        artist="Artist",
        album="Album",
        duration_seconds=200,
        added_at=datetime(2026, 6, 1, 12, 0, tzinfo=UTC),
        acquisition_status=AcquisitionStatus.READY,
        audio_ref=_AUDIO_REF,
    )


@pytest.fixture(scope="module")
def postgres_url() -> Iterator[str]:
    with PostgresContainer("postgres:16-alpine") as container:
        raw = container.get_connection_url()
        yield raw.replace("+psycopg2", "+asyncpg").replace("+psycopg", "+asyncpg")


@pytest.fixture
def music_dir(tmp_path: Path) -> Path:
    audio_path = tmp_path / _AUDIO_REF
    audio_path.parent.mkdir(parents=True, exist_ok=True)
    audio_path.write_bytes(_FAKE_MP3)
    return tmp_path


@pytest.fixture
def fresh_db(postgres_url: str) -> Iterator[str]:
    async def _setup() -> None:
        eng = create_async_engine(postgres_url)
        async with eng.begin() as conn:
            await conn.run_sync(Base.metadata.create_all)
        await eng.dispose()

    asyncio.run(_setup())
    yield postgres_url


def _seed(url: str, tracks: list[Track]) -> None:
    async def _do() -> None:
        eng = create_async_engine(url)
        factory = async_sessionmaker(eng, expire_on_commit=False)
        async with factory() as s:
            s.add_all([TrackRow.from_domain(t) for t in tracks])
            await s.commit()
        await eng.dispose()

    asyncio.run(_do())


def _client(db_url: str, music_dir: Path, user: UserId = _USER) -> TestClient:
    settings = Settings(  # type: ignore[call-arg]
        _env_file=None,
        database_url=db_url,
        env="test",
        music_dir=str(music_dir),
        supabase_project_url="https://fixture.supabase.co",
        supabase_jwt_jwks_url="https://fixture.supabase.co/auth/v1/keys",
    )
    app = create_app(settings=settings)
    app.dependency_overrides[current_user_id] = lambda: user
    return TestClient(app)


@pytest.mark.e2e
def test_stream_audio_returns_200_with_correct_headers_for_ready_track(
    fresh_db: str, music_dir: Path
) -> None:
    _seed(fresh_db, [_ready_track()])
    with _client(fresh_db, music_dir) as client:
        response = client.get(f"/v1/tracks/{_TRACK_ID}/audio")
    assert response.status_code == 200
    assert response.headers["content-type"] == "audio/mpeg"
    assert response.headers["accept-ranges"] == "bytes"
    assert int(response.headers["content-length"]) == len(_FAKE_MP3)
    assert response.headers["cache-control"] == "private, max-age=86400"
    assert response.content == _FAKE_MP3
