# mypy: warn_unused_ignores = False, disable_error_code = "no-any-return,untyped-decorator,type-arg,import-not-found"
"""RedisQueryCache — slice 28 testcontainers-backed integration tests."""

from __future__ import annotations

from datetime import timedelta
from typing import TYPE_CHECKING

import pytest
from redis.asyncio import Redis
from testcontainers.redis import RedisContainer

from altune.adapters.outbound.discovery.cache.redis_cache import (
    RedisQueryCache,
    cache_key,
)
from altune.domain.discovery.confidence import Confidence
from altune.domain.discovery.provider import ProviderName
from altune.domain.discovery.result_kind import ResultKind
from altune.domain.discovery.search_result import SearchResult
from altune.domain.discovery.source_ref import SourceRef

if TYPE_CHECKING:
    from collections.abc import AsyncIterator, Iterator


def _make_result(provider: ProviderName, ext_id: str = "1") -> SearchResult:
    return SearchResult(
        kind=ResultKind.TRACK,
        title="Let It Be",
        subtitle="The Beatles",
        image_url=None,
        confidence=Confidence.HIGH,
        sources=(SourceRef(provider=provider, external_id=ext_id, url=f"https://x/{ext_id}"),),
        extras={"isrc": "GBAYE0601477", "duration_seconds": 243},
    )


@pytest.fixture(scope="module")
def redis_url() -> Iterator[str]:
    with RedisContainer("redis:7-alpine") as container:
        host = container.get_container_host_ip()
        port = container.get_exposed_port(6379)
        yield f"redis://{host}:{port}/0"


@pytest.fixture
async def cache(redis_url: str) -> AsyncIterator[RedisQueryCache]:
    client = Redis.from_url(redis_url, decode_responses=True)
    await client.flushdb()
    yield RedisQueryCache(redis=client)
    await client.aclose()


@pytest.mark.integration
@pytest.mark.asyncio
async def test_redis_cache_round_trips_search_result_tuple(cache: RedisQueryCache) -> None:
    results = (
        _make_result(ProviderName.DEEZER, "1"),
        _make_result(ProviderName.DEEZER, "2"),
    )
    await cache.set(
        provider="deezer",
        query_norm="beatles",
        kinds=frozenset({ResultKind.TRACK}),
        results=results,
        ttl=timedelta(seconds=60),
    )
    got = await cache.get(
        provider="deezer",
        query_norm="beatles",
        kinds=frozenset({ResultKind.TRACK}),
    )
    assert got is not None
    cached_results, fetched_at = got
    assert len(cached_results) == 2
    assert cached_results[0].title == "Let It Be"
    assert cached_results[0].confidence is Confidence.HIGH
    assert cached_results[0].sources[0].provider is ProviderName.DEEZER
    assert cached_results[0].extras["isrc"] == "GBAYE0601477"
    # fetched_at was set by the cache itself, not by our payload.
    assert fetched_at.tzinfo is not None


@pytest.mark.integration
@pytest.mark.asyncio
async def test_redis_cache_returns_none_on_miss(cache: RedisQueryCache) -> None:
    got = await cache.get(
        provider="deezer",
        query_norm="never_set",
        kinds=frozenset({ResultKind.TRACK}),
    )
    assert got is None


@pytest.mark.integration
@pytest.mark.asyncio
async def test_redis_cache_distinguishes_per_kinds_keying(cache: RedisQueryCache) -> None:
    track_results = (_make_result(ProviderName.DEEZER, "t"),)
    await cache.set(
        provider="deezer",
        query_norm="beatles",
        kinds=frozenset({ResultKind.TRACK}),
        results=track_results,
        ttl=timedelta(seconds=60),
    )
    # Same query_norm + provider but different kinds → different key → miss.
    got = await cache.get(
        provider="deezer",
        query_norm="beatles",
        kinds=frozenset({ResultKind.ARTIST}),
    )
    assert got is None


@pytest.mark.integration
@pytest.mark.asyncio
async def test_redis_cache_respects_ttl(cache: RedisQueryCache) -> None:
    results = (_make_result(ProviderName.DEEZER),)
    await cache.set(
        provider="deezer",
        query_norm="ephemeral",
        kinds=frozenset({ResultKind.TRACK}),
        results=results,
        ttl=timedelta(milliseconds=10),  # rounded up to 1s by max(1, ...)
    )
    # Confirm the key exists and has a positive TTL.
    key = cache_key("v1", "deezer", "ephemeral", frozenset({ResultKind.TRACK}))
    ttl = await cache.redis.ttl(key)
    assert ttl >= 0


@pytest.mark.integration
@pytest.mark.asyncio
async def test_redis_cache_v2_prefix_does_not_see_v1_entries(cache: RedisQueryCache) -> None:
    results = (_make_result(ProviderName.DEEZER),)
    await cache.set(
        provider="deezer",
        query_norm="beatles",
        kinds=frozenset({ResultKind.TRACK}),
        results=results,
        ttl=timedelta(seconds=60),
    )
    # Bump to v2 and read the same logical query — should miss.
    cache.version = "v2"
    got = await cache.get(
        provider="deezer",
        query_norm="beatles",
        kinds=frozenset({ResultKind.TRACK}),
    )
    assert got is None


@pytest.mark.unit
def test_cache_key_includes_v1_prefix_and_kinds_sorted_csv() -> None:
    k = cache_key(
        "v1",
        "deezer",
        "beatles",
        frozenset({ResultKind.TRACK, ResultKind.ARTIST}),
    )
    # Kinds csv is sorted alphabetically: artist,track
    assert k.startswith("discovery:v1:deezer:artist,track:")


@pytest.mark.unit
def test_cache_key_changes_with_query_norm() -> None:
    a = cache_key("v1", "deezer", "beatles", frozenset({ResultKind.TRACK}))
    b = cache_key("v1", "deezer", "stones", frozenset({ResultKind.TRACK}))
    assert a != b


@pytest.mark.unit
def test_cache_key_changes_with_version() -> None:
    v1 = cache_key("v1", "deezer", "beatles", frozenset({ResultKind.TRACK}))
    v2 = cache_key("v2", "deezer", "beatles", frozenset({ResultKind.TRACK}))
    assert v1 != v2
