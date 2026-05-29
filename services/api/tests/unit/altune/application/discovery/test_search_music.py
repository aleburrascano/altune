"""SearchMusic use case — slice 16 spine + scatter-gather tests for later slices."""

from __future__ import annotations

import asyncio
from datetime import UTC, datetime
from uuid import UUID

import pytest
from tests._doubles.in_memory_search_history_repository import (
    InMemorySearchHistoryRepository,
)
from tests._doubles.in_memory_search_provider import InMemorySearchProvider

from altune.application.discovery.ports import ProviderSearchResponse
from altune.application.discovery.search_music import SearchMusic, SearchMusicInput
from altune.domain.discovery.confidence import Confidence
from altune.domain.discovery.provider import ProviderName
from altune.domain.discovery.provider_status import ProviderStatus
from altune.domain.discovery.result_kind import ResultKind
from altune.domain.discovery.search_result import SearchResult
from altune.domain.discovery.source_ref import SourceRef
from altune.domain.shared.user_id import UserId

_USER = UserId(UUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))


def _result(
    provider: ProviderName,
    title: str = "Let It Be",
    ext_id: str = "1",
    isrc: str | None = None,
) -> SearchResult:
    extras: dict[str, object] = {}
    if isrc is not None:
        extras["isrc"] = isrc
    return SearchResult(
        kind=ResultKind.TRACK,
        title=title,
        subtitle="The Beatles",
        image_url=None,
        confidence=Confidence.LOW,
        sources=(SourceRef(provider=provider, external_id=ext_id, url=f"https://x/{ext_id}"),),
        extras=extras,
    )


@pytest.mark.unit
@pytest.mark.asyncio
async def test_search_music_returns_dedup_ranked_results_and_persists_history() -> None:
    provider = InMemorySearchProvider(name="deezer", canned=(_result(ProviderName.DEEZER),))
    history = InMemorySearchHistoryRepository()
    use_case = SearchMusic(providers=[provider], history_repo=history)

    output = await use_case.execute(
        SearchMusicInput(
            raw_query="the beatles",
            user_id=_USER,
            kinds=frozenset({ResultKind.TRACK}),
        )
    )

    assert output.query == "the beatles"
    assert output.query_norm == "beatles"
    assert len(output.results) == 1
    assert output.providers[0].provider_name == "deezer"
    assert output.providers[0].status is ProviderStatus.OK
    assert output.partial is False

    listed = await history.list_distinct_recent(_USER, limit=10)
    assert len(listed) == 1


class _SlowProvider:
    name = "slow"

    async def search(
        self,
        query: str,
        kinds: frozenset[ResultKind],
        limit: int,
    ) -> ProviderSearchResponse:
        _ = (query, kinds, limit)
        await asyncio.sleep(3.0)
        return ProviderSearchResponse(
            provider_name=self.name, status=ProviderStatus.OK, results=(), latency_ms=0
        )

    async def lookup_by_url(self, url: str) -> SearchResult | None:
        _ = url
        return None


@pytest.mark.unit
@pytest.mark.asyncio
async def test_search_music_returns_partial_on_one_provider_timeout() -> None:
    ok = InMemorySearchProvider(name="deezer", canned=(_result(ProviderName.DEEZER),))
    slow = _SlowProvider()
    history = InMemorySearchHistoryRepository()
    use_case = SearchMusic(providers=[ok, slow], history_repo=history, per_source_timeout_s=0.05)

    output = await use_case.execute(
        SearchMusicInput(
            raw_query="the beatles",
            user_id=_USER,
            kinds=frozenset({ResultKind.TRACK}),
        )
    )

    assert output.partial is True
    statuses = {s.provider_name: s.status for s in output.providers}
    assert statuses["deezer"] is ProviderStatus.OK
    assert statuses["slow"] is ProviderStatus.TIMEOUT


class _ErrorProvider:
    name = "broken"

    async def search(
        self,
        query: str,
        kinds: frozenset[ResultKind],
        limit: int,
    ) -> ProviderSearchResponse:
        _ = (query, kinds, limit)
        raise RuntimeError("provider blew up")

    async def lookup_by_url(self, url: str) -> SearchResult | None:
        _ = url
        return None


@pytest.mark.unit
@pytest.mark.asyncio
async def test_search_music_maps_raised_exception_to_error_status() -> None:
    broken = _ErrorProvider()
    history = InMemorySearchHistoryRepository()
    use_case = SearchMusic(providers=[broken], history_repo=history)

    output = await use_case.execute(
        SearchMusicInput(
            raw_query="the beatles",
            user_id=_USER,
            kinds=frozenset({ResultKind.TRACK}),
        )
    )

    assert output.partial is True
    assert output.providers[0].status is ProviderStatus.ERROR


@pytest.mark.unit
@pytest.mark.asyncio
async def test_search_music_persists_history_even_on_partial_response() -> None:
    broken = _ErrorProvider()
    history = InMemorySearchHistoryRepository()
    use_case = SearchMusic(providers=[broken], history_repo=history)

    await use_case.execute(
        SearchMusicInput(
            raw_query="the beatles",
            user_id=_USER,
            kinds=frozenset({ResultKind.TRACK}),
        )
    )
    listed = await history.list_distinct_recent(_USER, limit=10)
    assert len(listed) == 1


class _FailingHistory:
    async def insert(self, entry: object) -> None:
        _ = entry
        raise RuntimeError("db down")

    async def trim_to_n(self, user_id: object, n: int) -> None:
        _ = (user_id, n)

    async def list_distinct_recent(self, user_id: object, limit: int) -> tuple[object, ...]:
        _ = (user_id, limit)
        return ()


@pytest.mark.unit
@pytest.mark.asyncio
async def test_search_music_returns_200_when_history_insert_raises() -> None:
    provider = InMemorySearchProvider(name="deezer", canned=(_result(ProviderName.DEEZER),))
    use_case = SearchMusic(providers=[provider], history_repo=_FailingHistory())  # type: ignore[arg-type]

    output = await use_case.execute(
        SearchMusicInput(
            raw_query="the beatles",
            user_id=_USER,
            kinds=frozenset({ResultKind.TRACK}),
        )
    )
    # Search still succeeded; history failure didn't propagate.
    assert len(output.results) == 1
    assert output.providers[0].status is ProviderStatus.OK


_NOW = datetime(2026, 5, 27, 12, 0, tzinfo=UTC)


@pytest.mark.unit
@pytest.mark.asyncio
async def test_search_music_calls_providers_in_parallel_not_serially() -> None:
    # Three providers each delay 0.1s. If serial, total >= 0.3s. If
    # parallel via asyncio.gather, total ~= 0.1s. We assert < 0.25s with
    # generous headroom for scheduler jitter.
    delay = 0.1
    p_a = InMemorySearchProvider(
        name="a", canned=(_result(ProviderName.DEEZER, ext_id="a"),), delay_s=delay
    )
    p_b = InMemorySearchProvider(
        name="b", canned=(_result(ProviderName.MUSICBRAINZ, ext_id="b"),), delay_s=delay
    )
    p_c = InMemorySearchProvider(
        name="c", canned=(_result(ProviderName.LASTFM, ext_id="c"),), delay_s=delay
    )
    history = InMemorySearchHistoryRepository()
    use_case = SearchMusic(
        providers=[p_a, p_b, p_c], history_repo=history, per_source_timeout_s=1.0
    )

    loop = asyncio.get_event_loop()
    start = loop.time()
    output = await use_case.execute(
        SearchMusicInput(
            raw_query="parallel",
            user_id=_USER,
            kinds=frozenset({ResultKind.TRACK}),
        )
    )
    elapsed = loop.time() - start

    assert elapsed < 0.25, f"providers ran serially (elapsed={elapsed:.3f}s)"
    assert output.partial is False
    assert {s.provider_name for s in output.providers} == {"a", "b", "c"}


def _distinct_result(
    provider: ProviderName, title: str, subtitle: str, ext_id: str
) -> SearchResult:
    """Result variant with distinct subtitle so dedup doesn't merge across providers."""
    return SearchResult(
        kind=ResultKind.TRACK,
        title=title,
        subtitle=subtitle,
        image_url=None,
        confidence=Confidence.LOW,
        sources=(SourceRef(provider=provider, external_id=ext_id, url=f"https://x/{ext_id}"),),
        extras={},
    )


@pytest.mark.unit
@pytest.mark.asyncio
async def test_search_music_one_failure_does_not_cancel_sibling_providers() -> None:
    # If one provider raises mid-flight, the others must still complete
    # and contribute results (TaskGroup-style cancellation would cancel
    # siblings; asyncio.gather with per-task try/except does not).
    # Both surviving results share the queried artist so they clear the
    # relevance floor — the floor is exercised elsewhere; this test is about
    # sibling survival when one provider raises.
    ok_a = InMemorySearchProvider(
        name="a",
        canned=(_distinct_result(ProviderName.DEEZER, "Apple Pie", "The Band", "a"),),
    )
    broken = InMemorySearchProvider(name="broken", raises=RuntimeError("boom"))
    ok_c = InMemorySearchProvider(
        name="c",
        canned=(_distinct_result(ProviderName.LASTFM, "Cherry Bombs", "The Band", "c"),),
    )
    history = InMemorySearchHistoryRepository()
    use_case = SearchMusic(
        providers=[ok_a, broken, ok_c], history_repo=history, per_source_timeout_s=1.0
    )

    output = await use_case.execute(
        SearchMusicInput(
            raw_query="the band",
            user_id=_USER,
            kinds=frozenset({ResultKind.TRACK}),
        )
    )

    statuses = {s.provider_name: s.status for s in output.providers}
    assert statuses["a"] is ProviderStatus.OK
    assert statuses["broken"] is ProviderStatus.ERROR
    assert statuses["c"] is ProviderStatus.OK
    titles = {r.title for r in output.results}
    assert titles == {"Apple Pie", "Cherry Bombs"}
    assert output.partial is True


@pytest.mark.unit
@pytest.mark.asyncio
async def test_search_music_passes_through_rate_limited_status_from_adapter() -> None:
    # Adapter returned RATE_LIMITED (e.g. 429); the use case must preserve
    # the distinct status (not collapse it into ERROR).
    rate_limited = InMemorySearchProvider(name="deezer", status=ProviderStatus.RATE_LIMITED)
    history = InMemorySearchHistoryRepository()
    use_case = SearchMusic(providers=[rate_limited], history_repo=history)

    output = await use_case.execute(
        SearchMusicInput(raw_query="q", user_id=_USER, kinds=frozenset({ResultKind.TRACK}))
    )
    assert output.providers[0].status is ProviderStatus.RATE_LIMITED
    assert output.providers[0].result_count == 0
    assert output.partial is True


@pytest.mark.unit
@pytest.mark.asyncio
async def test_search_music_skips_provider_when_breaker_open() -> None:
    # Drive the deezer breaker open with 5 raised exceptions, then verify
    # the next call short-circuits with CIRCUIT_OPEN before the provider
    # is even invoked.
    broken = InMemorySearchProvider(name="deezer", raises=RuntimeError("boom"))
    history = InMemorySearchHistoryRepository()
    use_case = SearchMusic(providers=[broken], history_repo=history)
    for _ in range(5):
        await use_case.execute(
            SearchMusicInput(
                raw_query="q",
                user_id=_USER,
                kinds=frozenset({ResultKind.TRACK}),
            )
        )
    # Replace `raises` with a canned response that would normally be OK;
    # if the breaker correctly short-circuits, this canned data is never
    # consumed and the status is CIRCUIT_OPEN.
    broken.raises = None
    broken.canned = (_result(ProviderName.DEEZER),)
    output = await use_case.execute(
        SearchMusicInput(
            raw_query="q",
            user_id=_USER,
            kinds=frozenset({ResultKind.TRACK}),
        )
    )
    assert output.providers[0].status is ProviderStatus.CIRCUIT_OPEN
    assert output.providers[0].result_count == 0
    assert output.results == ()


@pytest.mark.unit
@pytest.mark.asyncio
async def test_search_music_rate_limited_does_not_count_toward_breaker() -> None:
    # 10 consecutive rate_limited responses; breaker must stay closed
    # (rate-limited is a calling-pattern signal, not a provider-health one).
    rate_limited = InMemorySearchProvider(name="deezer", status=ProviderStatus.RATE_LIMITED)
    history = InMemorySearchHistoryRepository()
    use_case = SearchMusic(providers=[rate_limited], history_repo=history)
    for _ in range(10):
        output = await use_case.execute(
            SearchMusicInput(
                raw_query="q",
                user_id=_USER,
                kinds=frozenset({ResultKind.TRACK}),
            )
        )
        assert output.providers[0].status is ProviderStatus.RATE_LIMITED
    # Switch to success; breaker should not have tripped, so the call goes
    # through immediately.
    rate_limited.status = ProviderStatus.OK
    rate_limited.canned = (_result(ProviderName.DEEZER),)
    final = await use_case.execute(
        SearchMusicInput(
            raw_query="q",
            user_id=_USER,
            kinds=frozenset({ResultKind.TRACK}),
        )
    )
    assert final.providers[0].status is ProviderStatus.OK


@pytest.mark.unit
@pytest.mark.asyncio
async def test_search_music_full_warm_cache_hits_skip_live_calls() -> None:
    # Pre-warm the cache for both providers; live providers would raise
    # if called. The cache hit must skip them entirely.
    from datetime import timedelta as _td

    from tests._doubles.in_memory_query_cache import InMemoryQueryCache

    cache = InMemoryQueryCache()
    cached_result = _result(ProviderName.DEEZER, title="Cached Deezer Track", ext_id="d1")
    await cache.set(
        provider="deezer",
        query_norm="beatles",
        kinds=frozenset({ResultKind.TRACK}),
        results=(cached_result,),
        ttl=_td(seconds=60),
    )
    landmine = InMemorySearchProvider(name="deezer", raises=RuntimeError("should not be called"))
    history = InMemorySearchHistoryRepository()
    use_case = SearchMusic(providers=[landmine], history_repo=history, cache=cache)

    output = await use_case.execute(
        SearchMusicInput(
            raw_query="the beatles",
            user_id=_USER,
            kinds=frozenset({ResultKind.TRACK}),
        )
    )

    assert output.cache_hit is True
    assert output.cache_fetched_at is not None
    assert output.providers[0].status is ProviderStatus.OK
    assert output.providers[0].latency_ms == 0
    assert output.results[0].title == "Cached Deezer Track"


@pytest.mark.unit
@pytest.mark.asyncio
async def test_search_music_cache_miss_writes_to_cache_after_live_call() -> None:
    from tests._doubles.in_memory_query_cache import InMemoryQueryCache

    cache = InMemoryQueryCache()
    live = InMemorySearchProvider(
        name="deezer",
        canned=(_result(ProviderName.DEEZER, title="Live Track", ext_id="L1"),),
    )
    history = InMemorySearchHistoryRepository()
    use_case = SearchMusic(providers=[live], history_repo=history, cache=cache)

    output = await use_case.execute(
        SearchMusicInput(
            raw_query="the beatles",
            user_id=_USER,
            kinds=frozenset({ResultKind.TRACK}),
        )
    )

    assert output.cache_hit is False
    # The cache must now contain the result.
    stored = await cache.get("deezer", "beatles", frozenset({ResultKind.TRACK}))
    assert stored is not None
    cached_results, _ = stored
    assert cached_results[0].title == "Live Track"


@pytest.mark.unit
@pytest.mark.asyncio
async def test_search_music_mixed_warm_uses_cache_for_warm_live_for_expired() -> None:
    # One provider's entry is cached; the other has none. Both run; the
    # cached one returns instantly with latency_ms=0; the other does a
    # live call.
    from datetime import timedelta as _td

    from tests._doubles.in_memory_query_cache import InMemoryQueryCache

    cache = InMemoryQueryCache()
    await cache.set(
        provider="deezer",
        query_norm="beatles",
        kinds=frozenset({ResultKind.TRACK}),
        results=(_result(ProviderName.DEEZER, title="Cached Deezer", ext_id="cD"),),
        ttl=_td(seconds=60),
    )
    deezer_landmine = InMemorySearchProvider(
        name="deezer", raises=RuntimeError("warm; should not be called")
    )
    mb_live = InMemorySearchProvider(
        name="musicbrainz",
        canned=(_result(ProviderName.MUSICBRAINZ, title="Live MB", ext_id="L_mb"),),
    )
    history = InMemorySearchHistoryRepository()
    use_case = SearchMusic(providers=[deezer_landmine, mb_live], history_repo=history, cache=cache)

    output = await use_case.execute(
        SearchMusicInput(
            raw_query="the beatles",
            user_id=_USER,
            kinds=frozenset({ResultKind.TRACK}),
        )
    )

    statuses = {s.provider_name: s for s in output.providers}
    assert statuses["deezer"].latency_ms == 0  # cache served
    assert statuses["musicbrainz"].status is ProviderStatus.OK
    assert output.cache_hit is True
    # mb result is now also cached.
    mb_stored = await cache.get("musicbrainz", "beatles", frozenset({ResultKind.TRACK}))
    assert mb_stored is not None


@pytest.mark.unit
@pytest.mark.asyncio
async def test_search_music_passes_through_error_status_from_adapter() -> None:
    # Adapter returned ERROR (e.g. 5xx); the use case must preserve it as
    # ERROR (distinct from a raised exception, though both map to ERROR).
    error_provider = InMemorySearchProvider(name="deezer", status=ProviderStatus.ERROR)
    history = InMemorySearchHistoryRepository()
    use_case = SearchMusic(providers=[error_provider], history_repo=history)

    output = await use_case.execute(
        SearchMusicInput(raw_query="q", user_id=_USER, kinds=frozenset({ResultKind.TRACK}))
    )
    assert output.providers[0].status is ProviderStatus.ERROR
    assert output.partial is True
