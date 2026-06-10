# mypy: ignore_errors = True
"""Redis wiring in the app lifespan — integration half (testcontainers).

redis_url set → lifespan creates the async Redis client and activates the
previously dormant cache layer: RedisQueryCache (per-provider search cache)
plus the content-validation and fetch-success quality gates.
"""

from __future__ import annotations

from typing import TYPE_CHECKING

import pytest
from fastapi.testclient import TestClient
from testcontainers.redis import RedisContainer

from altune.adapters.outbound.discovery.cache.content_validation_cache import (
    RedisContentValidationCache,
)
from altune.adapters.outbound.discovery.cache.fetch_success_store import (
    RedisFetchSuccessStore,
)
from altune.adapters.outbound.discovery.cache.redis_cache import RedisQueryCache
from altune.platform.app import create_app
from altune.platform.config import Settings

if TYPE_CHECKING:
    from collections.abc import Iterator


@pytest.fixture(scope="module")
def redis_url() -> Iterator[str]:
    with RedisContainer("redis:7-alpine") as container:
        host = container.get_container_host_ip()
        port = container.get_exposed_port(6379)
        yield f"redis://{host}:{port}/0"


@pytest.mark.integration
def test_lifespan_with_redis_url_activates_cache_layer(redis_url: str) -> None:
    settings = Settings(
        _env_file=None,
        env="test",
        supabase_project_url="https://fixture.supabase.co",
        supabase_jwt_jwks_url="https://fixture.supabase.co/auth/v1/keys",
        redis_url=redis_url,
    )
    app = create_app(settings=settings)
    with TestClient(app):
        assert app.state.redis is not None
        assert isinstance(app.state.discovery_cache, RedisQueryCache)
        assert isinstance(
            app.state.discovery_content_validation_cache, RedisContentValidationCache
        )
        assert isinstance(app.state.discovery_fetch_success_store, RedisFetchSuccessStore)
