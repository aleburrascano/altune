# mypy: ignore_errors = True
"""CachedMbidResolver — Redis-backed decorator around the MB URL lookup."""

from __future__ import annotations

from typing import TYPE_CHECKING

import pytest
from altune.adapters.outbound.discovery.cache.mbid_cache import (
    CachedMbidResolver,
    mbid_cache_key,
)
from redis.asyncio import Redis
from testcontainers.redis import RedisContainer

if TYPE_CHECKING:
    from collections.abc import AsyncIterator, Iterator

_URL = "https://www.deezer.com/artist/234701081"
_MBID = "0a68f3b5-79c2-4f81-a7bc-ebc977602e86"


class _CountingResolver:
    def __init__(self, canned: str | None) -> None:
        self.canned = canned
        self.calls = 0

    async def resolve(self, provider_url: str) -> str | None:
        self.calls += 1
        return self.canned


class _ExplodingRedis:
    """Every operation raises — simulates Redis being down."""

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
async def test_mbid_cache_hit_skips_inner_resolver(redis: Redis) -> None:
    inner = _CountingResolver(canned=_MBID)
    resolver = CachedMbidResolver(inner=inner, redis=redis)

    first = await resolver.resolve(_URL)
    second = await resolver.resolve(_URL)

    assert first == _MBID
    assert second == _MBID
    assert inner.calls == 1


@pytest.mark.integration
@pytest.mark.asyncio
async def test_mbid_cache_negative_result_is_cached(redis: Redis) -> None:
    inner = _CountingResolver(canned=None)
    resolver = CachedMbidResolver(inner=inner, redis=redis)

    first = await resolver.resolve(_URL)
    second = await resolver.resolve(_URL)

    assert first is None
    assert second is None
    assert inner.calls == 1


@pytest.mark.integration
@pytest.mark.asyncio
async def test_mbid_cache_ttls_positive_30d_negative_24h(redis: Redis) -> None:
    hit = CachedMbidResolver(inner=_CountingResolver(canned=_MBID), redis=redis)
    await hit.resolve(_URL)
    positive_ttl = await redis.ttl(mbid_cache_key(_URL))
    assert 29 * 86400 < positive_ttl <= 30 * 86400

    await redis.flushdb()
    miss = CachedMbidResolver(inner=_CountingResolver(canned=None), redis=redis)
    await miss.resolve(_URL)
    negative_ttl = await redis.ttl(mbid_cache_key(_URL))
    assert 23 * 3600 < negative_ttl <= 24 * 3600


@pytest.mark.integration
@pytest.mark.asyncio
async def test_mbid_cache_degrades_to_inner_when_redis_down() -> None:
    inner = _CountingResolver(canned=_MBID)
    resolver = CachedMbidResolver(inner=inner, redis=_ExplodingRedis())

    result = await resolver.resolve(_URL)

    assert result == _MBID
    assert inner.calls == 1
