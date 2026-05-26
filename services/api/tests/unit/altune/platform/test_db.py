"""Engine + sessionmaker factories return correctly-typed objects.

Real DB interaction (check_database hitting a live SELECT 1) is exercised
in tests/integration/ via testcontainers, not here.
"""

from __future__ import annotations

import pytest
from sqlalchemy.ext.asyncio import (
    AsyncEngine,
    AsyncSession,
    async_sessionmaker,
)

from altune.platform.db import create_engine, create_sessionmaker

_FAKE_URL = "postgresql+asyncpg://altune:dev@localhost:5432/altune"


@pytest.mark.unit
def test_create_engine_returns_async_engine_with_postgres_dialect() -> None:
    engine = create_engine(_FAKE_URL)
    assert isinstance(engine, AsyncEngine)
    assert engine.dialect.name == "postgresql"


@pytest.mark.unit
def test_create_sessionmaker_returns_async_factory() -> None:
    engine = create_engine(_FAKE_URL)
    factory = create_sessionmaker(engine)
    assert isinstance(factory, async_sessionmaker)


@pytest.mark.unit
def test_create_sessionmaker_disables_expire_on_commit() -> None:
    engine = create_engine(_FAKE_URL)
    factory = create_sessionmaker(engine)
    # async_sessionmaker stores config kwargs and applies them at session creation;
    # constructing one (no connect needed for this assertion) lets us verify the
    # AsyncSession was configured per the SQLAlchemy 2.0 asyncio guide.
    session: AsyncSession = factory()
    assert session.sync_session.expire_on_commit is False
