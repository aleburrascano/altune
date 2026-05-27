"""/health reports db=ok against a real Postgres.

Per ADR-0003 + the walking-skeleton verification step. Uses testcontainers
to spin an ephemeral Postgres 16 and asserts the lifespan-wired engine
runs SELECT 1 successfully.

Requires Docker Desktop running on the host (per ADR-0003 "What becomes
harder"). The test is skipped automatically if testcontainers can't reach
a docker daemon, so a docker-less environment fails clearly rather than
hanging.
"""

from __future__ import annotations

from typing import TYPE_CHECKING

import pytest
from fastapi.testclient import TestClient
from testcontainers.postgres import PostgresContainer

from altune.platform.app import create_app
from altune.platform.config import Settings

if TYPE_CHECKING:
    from collections.abc import Iterator


@pytest.fixture(scope="module")
def postgres_url() -> Iterator[str]:
    """Spin an ephemeral Postgres 16 and yield an asyncpg-compatible URL."""
    with PostgresContainer("postgres:16-alpine") as container:
        raw = container.get_connection_url()
        # testcontainers defaults to psycopg2 in its URL; rewrite for asyncpg.
        asyncpg = raw.replace("+psycopg2", "+asyncpg").replace("+psycopg", "+asyncpg")
        yield asyncpg


@pytest.mark.integration
def test_health_reports_db_ok_against_real_postgres(postgres_url: str) -> None:
    settings = Settings(_env_file=None, database_url=postgres_url)  # type: ignore[call-arg]
    app = create_app(settings=settings)
    # Sync TestClient context manager runs the lifespan, which creates the
    # engine + sessionmaker on app.state.
    with TestClient(app) as client:
        response = client.get("/health")
    assert response.status_code == 200
    body = response.json()
    assert body["status"] == "ok"
    assert body["db"] == "ok"
    assert "version" in body


@pytest.mark.integration
def test_health_reports_db_not_configured_when_url_unset() -> None:
    """When DATABASE_URL is None, lifespan skips DB init and /health says so.

    Doesn't need a postgres container — purely the not-configured branch.
    """
    settings = Settings(_env_file=None, database_url=None)  # type: ignore[call-arg]
    app = create_app(settings=settings)
    with TestClient(app) as client:
        response = client.get("/health")
    assert response.status_code == 200
    body = response.json()
    assert body["status"] == "ok"
    assert body["db"] == "not_configured"
