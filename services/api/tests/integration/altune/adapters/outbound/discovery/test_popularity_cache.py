# mypy: ignore_errors = True
"""CachedPopularityResolver — Redis cache around the Last.fm getInfo back-fill.

Makes the uniform popularity basis durable (one successful lookup sticks for
days) and cuts the ~25 live getInfo calls per search down to cache misses.
"""

from __future__ import annotations

from typing import TYPE_CHECKING

import pytest
from altune.adapters.outbound.discovery.cache.popularity_cache import (
    CachedPopularityResolver,
    popularity_cache_key,
)
from redis.asyncio import Redis
from testcontainers.redis import RedisContainer

from altune.domain.discovery.result_kind import ResultKind

if TYPE_CHECKING:
    from collections.abc import AsyncIterator, Iterator


class _CountingResolver:
    def __init__(self, canned: float | None) -> None:
        self.canned = canned
        self.calls = 0

    async def resolve_popularity(
        self, kind: ResultKind, title: str, subtitle: str | None
    ) -> float | None:
        self.calls += 1
        return self.canned


class _ExplodingRedis:
    def __getattr__(self, name: str) -> object:
        async def _boom(*args: object, **kwargs: object) -> object:
            raise ConnectionError("redis down")

        return _boom


@pytest.fixture(scope="module")
def redis_url() -> Iterator[str]:
    with RedisContainer("redis:7-alpine") as container:
        host = container.get_container_host_ip()
        port = container.get_exposed_port(6379)
        yield f"redis://{host}:{port}/0"


@pytest.fixture
async def redis(redis_url: str) -> AsyncIterator[Redis]:
    client = Redis.from_url(redis_url, decode_responses=True)
    await client.flushdb()
    yield client
    await client.aclose()


@pytest.mark.integration
@pytest.mark.asyncio
async def test_popularity_cache_hit_skips_inner(redis: Redis) -> None:
    inner = _CountingResolver(canned=0.75)
    resolver = CachedPopularityResolver(inner=inner, redis=redis)

    first = await resolver.resolve_popularity(ResultKind.TRACK, "Super Shy", "NewJeans")
    second = await resolver.resolve_popularity(ResultKind.TRACK, "Super Shy", "NewJeans")

    assert first == 0.75
    assert second == 0.75
    assert inner.calls == 1


@pytest.mark.integration
@pytest.mark.asyncio
async def test_popularity_cache_negative_result_is_cached(redis: Redis) -> None:
    inner = _CountingResolver(canned=None)
    resolver = CachedPopularityResolver(inner=inner, redis=redis)

    first = await resolver.resolve_popularity(ResultKind.TRACK, "Obscure", "Edit Hub")
    second = await resolver.resolve_popularity(ResultKind.TRACK, "Obscure", "Edit Hub")

    assert first is None
    assert second is None
    assert inner.calls == 1


@pytest.mark.integration
@pytest.mark.asyncio
async def test_popularity_cache_ttls_positive_7d_negative_2h(redis: Redis) -> None:
    hit = CachedPopularityResolver(inner=_CountingResolver(canned=0.75), redis=redis)
    await hit.resolve_popularity(ResultKind.TRACK, "Super Shy", "NewJeans")
    positive_ttl = await redis.ttl(popularity_cache_key(ResultKind.TRACK, "Super Shy", "NewJeans"))
    assert 6 * 86400 < positive_ttl <= 7 * 86400

    await redis.flushdb()
    miss = CachedPopularityResolver(inner=_CountingResolver(canned=None), redis=redis)
    await miss.resolve_popularity(ResultKind.TRACK, "Obscure", "Edit Hub")
    negative_ttl = await redis.ttl(popularity_cache_key(ResultKind.TRACK, "Obscure", "Edit Hub"))
    assert 1 * 3600 < negative_ttl <= 2 * 3600


@pytest.mark.integration
@pytest.mark.asyncio
async def test_popularity_cache_degrades_to_inner_when_redis_down() -> None:
    inner = _CountingResolver(canned=0.5)
    resolver = CachedPopularityResolver(inner=inner, redis=_ExplodingRedis())

    result = await resolver.resolve_popularity(ResultKind.TRACK, "Super Shy", "NewJeans")

    assert result == 0.5
    assert inner.calls == 1
