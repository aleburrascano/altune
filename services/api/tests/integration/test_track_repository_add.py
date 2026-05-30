"""Integration: SqlAlchemyTrackRepository.add — create + dedup on real Postgres.

Exercises the ON CONFLICT (user_id, dedup_key) DO NOTHING path: a fresh save
inserts and returns created=True; an identical save (case-insensitive natural
key) returns the existing row with created=False and writes no duplicate.
Covers spec AC#5, AC#7.
"""

from __future__ import annotations

from collections.abc import AsyncIterator, Iterator
from datetime import UTC, datetime
from pathlib import Path
from uuid import UUID, uuid4

import pytest
import pytest_asyncio
from alembic import command
from alembic.config import Config
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker, create_async_engine
from testcontainers.postgres import PostgresContainer

from altune.adapters.outbound.persistence.catalog.track_repository import (
    SqlAlchemyTrackRepository,
)
from altune.domain.catalog.acquisition_status import AcquisitionStatus
from altune.domain.catalog.track import Track
from altune.domain.catalog.track_id import TrackId
from altune.domain.shared.user_id import UserId

_USER = UserId(UUID("00000000-0000-0000-0000-0000000000c3"))
_BASE = datetime(2026, 5, 1, 12, 0, tzinfo=UTC)


@pytest.fixture(scope="module")
def _pg_url() -> Iterator[str]:
    with PostgresContainer("postgres:16-alpine") as pg:
        url = pg.get_connection_url()
        root = Path(__file__).resolve().parents[2]
        cfg = Config(str(root / "alembic.ini"))
        cfg.set_main_option("script_location", str(root / "migrations"))
        cfg.set_main_option("sqlalchemy.url", url)
        command.upgrade(cfg, "head")
        yield url


@pytest_asyncio.fixture
async def _session(_pg_url: str) -> AsyncIterator[AsyncSession]:
    async_url = _pg_url.replace("postgresql+psycopg2://", "postgresql+asyncpg://").replace(
        "postgresql://", "postgresql+asyncpg://"
    )
    engine = create_async_engine(async_url)
    async with async_sessionmaker(engine, expire_on_commit=False)() as session:
        yield session
    await engine.dispose()


def _track(*, title: str = "Song", artist: str = "Artist", album: str | None = "Album") -> Track:
    return Track(
        id=TrackId(uuid4()),
        user_id=_USER,
        title=title,
        artist=artist,
        album=album,
        duration_seconds=180,
        added_at=_BASE,
        artwork_url="https://img.example/c.jpg",
        acquisition_status=AcquisitionStatus.PENDING,
    )


@pytest.mark.integration
async def test_add_persists_new_track_created_true(_session: AsyncSession) -> None:
    repo = SqlAlchemyTrackRepository(_session)

    persisted, created = await repo.add(_track(title="Unique One"))
    await _session.commit()

    assert created is True
    assert persisted.acquisition_status is AcquisitionStatus.PENDING
    items, _ = await repo.list_for_user(_USER, limit=50, offset=0)
    assert any(t.id == persisted.id for t in items)


@pytest.mark.integration
async def test_add_duplicate_returns_existing_created_false(_session: AsyncSession) -> None:
    repo = SqlAlchemyTrackRepository(_session)

    first, c1 = await repo.add(_track(title="Dup Track", artist="A", album="Al"))
    await _session.commit()
    again, c2 = await repo.add(_track(title="dup track", artist="a", album="al"))
    await _session.commit()

    assert c1 is True
    assert c2 is False
    assert again.id == first.id
