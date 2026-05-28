# mypy: warn_unused_ignores = False, disable_error_code = "no-any-return,untyped-decorator,type-arg,import-not-found,call-arg"
"""Slice 47 — full e2e happy-path for GET /v1/discovery/search.

Spins testcontainers Postgres, runs alembic, constructs the FastAPI
app with respx-mocked providers, and drives the route via sync
TestClient (which runs the lifespan). Auth is bypassed via
dependency_overrides per the pattern in test_tracks_route.py.
"""

from __future__ import annotations

import json
import os
from pathlib import Path
from typing import TYPE_CHECKING
from uuid import UUID

import httpx
import pytest
import respx
from fastapi.testclient import TestClient
from testcontainers.postgres import PostgresContainer

from altune.domain.shared.user_id import UserId
from altune.platform.app import create_app
from altune.platform.auth import current_user_id
from altune.platform.config import Settings

if TYPE_CHECKING:
    from collections.abc import Iterator

_FIXTURES = Path(__file__).resolve().parents[3] / "integration" / "fixtures" / "discovery"
_USER = UserId(UUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))


@pytest.fixture(scope="module")
def upgraded_postgres_url() -> Iterator[str]:
    """Spin Postgres + run alembic head (sync). Skip when Docker is down."""
    try:
        container = PostgresContainer("postgres:16-alpine")
        container.start()
    except Exception as exc:
        pytest.skip(f"Docker unavailable: {exc}")
    try:
        raw = container.get_connection_url()
        asyncpg_url = raw.replace("+psycopg2", "+asyncpg").replace(
            "+psycopg", "+asyncpg"
        )
        from alembic import command
        from alembic.config import Config

        prior = os.environ.get("DATABASE_URL")
        os.environ["DATABASE_URL"] = asyncpg_url
        try:
            cfg = Config(str(Path(__file__).resolve().parents[4] / "alembic.ini"))
            cfg.set_main_option("sqlalchemy.url", asyncpg_url)
            command.upgrade(cfg, "head")
            yield asyncpg_url
        finally:
            if prior is None:
                os.environ.pop("DATABASE_URL", None)
            else:
                os.environ["DATABASE_URL"] = prior
    finally:
        container.stop()


@pytest.fixture
def deezer_payload() -> dict:
    return json.loads((_FIXTURES / "deezer" / "track_search.json").read_text(encoding="utf-8"))


def _client_for(url: str) -> TestClient:
    settings = Settings(
        _env_file=None,
        database_url=url,
        env="test",
        supabase_project_url="https://fixture.supabase.co",
        supabase_jwt_jwks_url="https://fixture.supabase.co/auth/v1/keys",
    )
    app = create_app(settings=settings)
    app.dependency_overrides[current_user_id] = lambda: _USER
    return TestClient(app)


@pytest.mark.e2e
def test_search_round_trip_returns_merged_shape(
    upgraded_postgres_url: str,
    deezer_payload: dict,
) -> None:
    with respx.mock(assert_all_called=False) as router:
        router.get("https://api.deezer.com/search/track").mock(
            return_value=httpx.Response(200, json=deezer_payload)
        )
        router.get("https://musicbrainz.org/ws/2/recording").mock(
            return_value=httpx.Response(200, json={"recordings": []})
        )
        router.get("https://ws.audioscrobbler.com/2.0/").mock(
            return_value=httpx.Response(403, text="no api key configured")
        )

        with _client_for(upgraded_postgres_url) as client:
            response = client.get(
                "/v1/discovery/search",
                params={"q": "the beatles", "limit": "5"},
            )

    assert response.status_code == 200
    payload = response.json()
    assert payload["query"] == "the beatles"
    assert payload["query_norm"] == "beatles"
    # Deezer returned 5 results; cap to limit=5 after dedup.
    assert len(payload["results"]) > 0
    # Providers tuple must surface per-provider statuses.
    provider_names = {p["provider"] for p in payload["providers"]}
    assert "deezer" in provider_names
    # AC#5: partial=True when any provider is not ok.
    if any(p["status"] != "ok" for p in payload["providers"]):
        assert payload["partial"] is True


@pytest.mark.e2e
def test_search_history_endpoint_returns_persisted_query(
    upgraded_postgres_url: str,
    deezer_payload: dict,
) -> None:
    with respx.mock(assert_all_called=False) as router:
        router.get("https://api.deezer.com/search/track").mock(
            return_value=httpx.Response(200, json=deezer_payload)
        )
        router.get("https://musicbrainz.org/ws/2/recording").mock(
            return_value=httpx.Response(200, json={"recordings": []})
        )
        router.get("https://ws.audioscrobbler.com/2.0/").mock(
            return_value=httpx.Response(403, text="")
        )

        with _client_for(upgraded_postgres_url) as client:
            client.get("/v1/discovery/search", params={"q": "stones", "limit": "5"})
            history = client.get("/v1/discovery/search-history")

    assert history.status_code == 200
    payload = history.json()
    assert payload["total"] >= 1
    queries = {item["query"] for item in payload["items"]}
    assert "stones" in queries


@pytest.mark.e2e
def test_search_returns_422_for_empty_q(upgraded_postgres_url: str) -> None:
    with _client_for(upgraded_postgres_url) as client:
        response = client.get("/v1/discovery/search", params={"q": ""})
    assert response.status_code == 422


@pytest.mark.e2e
def test_search_returns_422_for_limit_above_50(upgraded_postgres_url: str) -> None:
    with _client_for(upgraded_postgres_url) as client:
        response = client.get(
            "/v1/discovery/search", params={"q": "beatles", "limit": "100"}
        )
    assert response.status_code == 422
