# mypy: ignore_errors = True
"""RedisArtworkCache — final artwork-outcome cache (positive + negative)."""

from __future__ import annotations

from typing import TYPE_CHECKING

import pytest
from altune.adapters.outbound.discovery.cache.artwork_cache import (
    RedisArtworkCache,
    artwork_cache_key,
)
from redis.asyncio import Redis
from testcontainers.redis import RedisContainer

from altune.domain.discovery.result_kind import ResultKind

if TYPE_CHECKING:
    from collections.abc import AsyncIterator, Iterator

_URL = "https://images.genius.com/che.jpg"
_MBID = "0a68f3b5-79c2-4f81-a7bc-ebc977602e86"


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
async def test_artwork_cache_round_trips_positive_outcome(redis: Redis) -> None:
    cache = RedisArtworkCache(redis=redis)
    await cache.set(ResultKind.ARTIST, "Che", None, _MBID, _URL)

    entry = await cache.get(ResultKind.ARTIST, "Che", None, _MBID)

    assert entry is not None
    assert entry.url == _URL


@pytest.mark.integration
@pytest.mark.asyncio
async def test_artwork_cache_negative_outcome_distinct_from_miss(redis: Redis) -> None:
    cache = RedisArtworkCache(redis=redis)
    await cache.set(ResultKind.ARTIST, "Che", None, _MBID, None)

    negative = await cache.get(ResultKind.ARTIST, "Che", None, _MBID)
    miss = await cache.get(ResultKind.ARTIST, "Other", None, None)

    assert negative is not None
    assert negative.url is None
    assert miss is None


@pytest.mark.integration
@pytest.mark.asyncio
async def test_artwork_cache_ttls_positive_14d_negative_24h(redis: Redis) -> None:
    cache = RedisArtworkCache(redis=redis)

    await cache.set(ResultKind.ARTIST, "Che", None, _MBID, _URL)
    positive_ttl = await redis.ttl(artwork_cache_key(ResultKind.ARTIST, "Che", None, _MBID))
    assert 13 * 86400 < positive_ttl <= 14 * 86400

    await cache.set(ResultKind.ARTIST, "Nope", None, None, None)
    negative_ttl = await redis.ttl(artwork_cache_key(ResultKind.ARTIST, "Nope", None, None))
    assert 23 * 3600 < negative_ttl <= 24 * 3600


@pytest.mark.integration
@pytest.mark.asyncio
async def test_artwork_cache_degrades_on_redis_errors() -> None:
    cache = RedisArtworkCache(redis=_ExplodingRedis())

    entry = await cache.get(ResultKind.ARTIST, "Che", None, _MBID)
    await cache.set(ResultKind.ARTIST, "Che", None, _MBID, _URL)  # must not raise

    assert entry is None
