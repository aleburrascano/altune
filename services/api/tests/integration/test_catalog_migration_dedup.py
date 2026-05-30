"""Integration test: catalog acquisition+dedup migration applies on real Postgres.

Spins a throwaway Postgres via testcontainers, runs `alembic upgrade head`, and
asserts the `tracks` table gained the acquisition/dedup columns and the
UNIQUE(user_id, dedup_key) constraint behind spec AC#7.
"""

from __future__ import annotations

from collections.abc import Iterator
from pathlib import Path

import pytest
import sqlalchemy as sa
from sqlalchemy import create_engine
from testcontainers.postgres import PostgresContainer

from alembic import command
from alembic.config import Config


@pytest.fixture(scope="module")
def _pg_url() -> Iterator[str]:
    with PostgresContainer("postgres:16-alpine") as pg:
        yield pg.get_connection_url()


def _alembic_config(db_url: str) -> Config:
    root = Path(__file__).resolve().parents[2]
    cfg = Config(str(root / "alembic.ini"))
    cfg.set_main_option("script_location", str(root / "migrations"))
    cfg.set_main_option("sqlalchemy.url", db_url)
    return cfg


@pytest.mark.integration
def test_catalog_migration_adds_acquisition_columns_and_unique_dedup_index(_pg_url: str) -> None:
    command.upgrade(_alembic_config(_pg_url), "head")

    engine = create_engine(_pg_url)
    with engine.connect() as conn:
        insp = sa.inspect(conn)
        cols = {c["name"] for c in insp.get_columns("tracks")}
        assert {"artwork_url", "acquisition_status", "dedup_key"}.issubset(cols)

        constraint_names = {uc["name"] for uc in insp.get_unique_constraints("tracks")}
        index_names = {ix["name"] for ix in insp.get_indexes("tracks")}
        assert "uq_tracks_user_dedup" in (constraint_names | index_names)
