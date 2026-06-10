# mypy: ignore_errors = True
"""Redis wiring in the app lifespan — unit half (no container).

redis_url unset → the whole Redis layer stays off: no client, no query
cache, no validation/fetch-success caches. The app must still boot.
"""

from __future__ import annotations

import pytest
from fastapi.testclient import TestClient

from altune.platform.app import create_app
from altune.platform.config import Settings


def _settings(**overrides: object) -> Settings:
    return Settings(
        _env_file=None,
        env="test",
        supabase_project_url="https://fixture.supabase.co",
        supabase_jwt_jwks_url="https://fixture.supabase.co/auth/v1/keys",
        **overrides,
    )


@pytest.mark.unit
def test_lifespan_without_redis_url_leaves_redis_layer_off() -> None:
    app = create_app(settings=_settings())
    with TestClient(app):
        assert app.state.redis is None
        assert app.state.discovery_cache is None
        assert app.state.discovery_content_validation_cache is None
        assert app.state.discovery_fetch_success_store is None
