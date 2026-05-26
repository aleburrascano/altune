"""Async SQLAlchemy engine + session factory.

Per ADR-0003. The engine and sessionmaker are constructed once per app
lifespan (see platform/app.py); repositories acquire a session via a
FastAPI dependency that pulls the factory from app.state.

This module is intentionally framework-free: no FastAPI imports, no
Request dependency. The wiring lives in platform/app.py.
"""

from __future__ import annotations

import structlog
from sqlalchemy import text
from sqlalchemy.ext.asyncio import (
    AsyncEngine,
    AsyncSession,
    async_sessionmaker,
    create_async_engine,
)

log = structlog.get_logger(__name__)


def create_engine(database_url: str) -> AsyncEngine:
    """Construct the async engine for Postgres via asyncpg.

    pool_pre_ping=True trades a tiny per-checkout query for resilience
    against stale connections (Supabase + asyncpg occasionally drops
    long-idle sockets; ADR-0003 picked Supabase for prod, so this matters).
    """
    return create_async_engine(database_url, pool_pre_ping=True)


def create_sessionmaker(engine: AsyncEngine) -> async_sessionmaker[AsyncSession]:
    """Construct the session factory.

    expire_on_commit=False is the SQLAlchemy 2.0 async-recommended default;
    without it, accessing any attribute after commit triggers a lazy load
    that fails or surprises in async contexts.
    """
    return async_sessionmaker(engine, expire_on_commit=False)


async def check_database(sessionmaker: async_sessionmaker[AsyncSession]) -> bool:
    """Run SELECT 1 against the configured DB; return True iff it succeeds.

    Used by the /health endpoint. Connection errors and timeouts are caught
    and logged at WARNING; the caller decides what to report. Catching the
    broad Exception is intentional here (and noted with AIDEV-WARNING below)
    because the health probe must never raise into the response path.
    """
    # AIDEV-WARNING: broad except is deliberate — the health endpoint must
    # never raise. Any failure (connection refused, auth, DNS, timeout) maps
    # to "db not ok" with a structured warning log carrying the cause.
    try:
        async with sessionmaker() as session:
            result = await session.execute(text("SELECT 1"))
            # sqlalchemy.* is in mypy ignore_missing_imports so .scalar() is Any;
            # the bool() cast makes the return type explicit and intentional.
            return bool(result.scalar() == 1)
    except Exception as exc:
        log.warning("db_check_failed", error=str(exc), exc_type=type(exc).__name__)
        return False
