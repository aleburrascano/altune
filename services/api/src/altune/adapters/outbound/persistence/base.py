"""Shared SQLAlchemy declarative base for all persistence adapters.

All `*Row` models in `adapters/outbound/persistence/<context>/` inherit from
this Base so `Base.metadata` is the single source of truth for the schema —
useful for future alembic --autogenerate runs (not enabled in v1; migrations
are still hand-written) and for test-time `Base.metadata.create_all` that
bypasses alembic when integration tests want a fresh schema fast.
"""

from __future__ import annotations

from sqlalchemy.orm import DeclarativeBase


class Base(DeclarativeBase):  # type: ignore[misc, unused-ignore]  # sqlalchemy.* in mypy ignore_missing_imports → per-file hook sees Any, full-project resolves
    """SQLAlchemy 2.0 declarative base, shared across persistence adapters."""
