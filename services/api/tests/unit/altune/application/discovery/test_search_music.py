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


class _FakeArtworkResolver:
    """Stub ArtworkResolver that always returns a fixed cover URL."""

    async def resolve_artwork(
        self, kind: ResultKind, title: str, subtitle: str | None
    ) -> str | None:
        _ = (kind, title, subtitle)
        return "https://art.example/cover.jpg"


@pytest.mark.unit
@pytest.mark.asyncio
async def test_search_music_backfills_artwork_for_artless_results() -> None:
    artless = SearchResult(
        kind=ResultKind.TRACK,
        title="Rest in Bass",
        subtitle="Che",
        image_url=None,
        confidence=Confidence.LOW,
        sources=(
            SourceRef(provider=ProviderName.MUSICBRAINZ, external_id="mb", url="https://x/mb"),
        ),
        extras={},
    )
    provider = InMemorySearchProvider(name="musicbrainz", canned=(artless,))
    use_case = SearchMusic(
        providers=[provider],
        history_repo=InMemorySearchHistoryRepository(),
        artwork_resolver=_FakeArtworkResolver(),
    )
    output = await use_case.execute(
        SearchMusicInput(
            raw_query="rest in bass che",
            user_id=_USER,
            kinds=frozenset({ResultKind.TRACK}),
        )
    )
    assert output.results[0].image_url == "https://art.example/cover.jpg"


class _FakePopularityResolver:
    """Stub PopularityResolver returning a fixed popularity per subtitle."""

    def __init__(self, by_subtitle: dict[str, float]) -> None:
        self._by_subtitle = by_subtitle

    async def resolve_popularity(
        self, kind: ResultKind, title: str, subtitle: str | None
    ) -> float | None:
        _ = (kind, title)
        return self._by_subtitle.get(subtitle or "")


def _track(subtitle: str, ext_id: str) -> SearchResult:
    return SearchResult(
        kind=ResultKind.TRACK,
        title="Anthem",
        subtitle=subtitle,
        image_url="https://x/art.jpg",
        confidence=Confidence.LOW,
        sources=(
            SourceRef(provider=ProviderName.DEEZER, external_id=ext_id, url=f"https://x/{ext_id}"),
        ),
        extras={"popularity": 0.5},
    )


@pytest.mark.unit
@pytest.mark.asyncio
async def test_search_music_enriches_popularity_and_reranks() -> None:
    # Two equally-relevant tracks (same title => band 1.0, distinct artists =>
    # no merge). The resolver makes "Omega" far more popular, so after the
    # enrichment pass + rerank it overtakes "Alpha".
    provider = InMemorySearchProvider(
        name="deezer", canned=(_track("Alpha", "a"), _track("Omega", "b"))
    )
    use_case = SearchMusic(
        providers=[provider],
        history_repo=InMemorySearchHistoryRepository(),
        popularity_resolver=_FakePopularityResolver({"Alpha": 0.1, "Omega": 0.9}),
    )
    output = await use_case.execute(
        SearchMusicInput(raw_query="anthem", user_id=_USER, kinds=frozenset({ResultKind.TRACK}))
    )
    assert output.results[0].subtitle == "Omega"
    assert output.results[0].extras["popularity"] == 0.9


@pytest.mark.unit
@pytest.mark.asyncio
async def test_search_music_uses_quality_scorer_when_provided() -> None:
    from altune.application.discovery.quality_scorer import compute_quality_score

    rich = SearchResult(
        kind=ResultKind.TRACK,
        title="Track",
        subtitle="Artist",
        image_url="http://img",
        confidence=Confidence.LOW,
        sources=(
            SourceRef(provider=ProviderName.SOUNDCLOUD, external_id="sc1", url="https://sc/1"),
        ),
        extras={"isrc": "ISRC1", "duration_seconds": 200, "album": "Album"},
    )
    sparse = SearchResult(
        kind=ResultKind.TRACK,
        title="Track",
        subtitle="Artist",
        image_url=None,
        confidence=Confidence.LOW,
        sources=(
            SourceRef(provider=ProviderName.MUSICBRAINZ, external_id="mb1", url="https://mb/1"),
        ),
        extras={},
    )
    provider_rich = InMemorySearchProvider(name="soundcloud", canned=(rich,))
    provider_sparse = InMemorySearchProvider(name="musicbrainz", canned=(sparse,))
    use_case = SearchMusic(
        providers=[provider_rich, provider_sparse],
        history_repo=InMemorySearchHistoryRepository(),
        quality_scorer=compute_quality_score,
    )
    output = await use_case.execute(
        SearchMusicInput(
            raw_query="track artist",
            user_id=_USER,
            kinds=frozenset({ResultKind.TRACK}),
        )
    )
    assert output.results[0].extras.get("isrc") == "ISRC1"


class _FakeContentValidationCache:
    """In-memory content validation cache for testing."""

    def __init__(self, responses: dict[tuple[str, str], str] | None = None) -> None:
        from altune.domain.discovery.content_validation_status import ContentValidationStatus

        self._data: dict[tuple[str, str], ContentValidationStatus] = {}
        if responses:
            for key, val in responses.items():
                self._data[key] = ContentValidationStatus(val)

    async def get(self, provider: str, external_id: str) -> ContentValidationStatus:
        from altune.domain.discovery.content_validation_status import ContentValidationStatus

        return self._data.get((provider, external_id), ContentValidationStatus.UNKNOWN)

    async def record(
        self, provider: str, external_id: str, status: ContentValidationStatus
    ) -> None:
        self._data[(provider, external_id)] = status


@pytest.mark.unit
@pytest.mark.asyncio
async def test_result_with_all_sources_unfetchable_filtered() -> None:
    """AC#11: result whose every source has cached UNFETCHABLE is removed."""
    fetchable = SearchResult(
        kind=ResultKind.TRACK,
        title="Good Song",
        subtitle="Good Artist",
        image_url=None,
        confidence=Confidence.LOW,
        sources=(SourceRef(provider=ProviderName.DEEZER, external_id="d1", url="https://x/d1"),),
        extras={},
    )
    unfetchable = SearchResult(
        kind=ResultKind.TRACK,
        title="Ghost Song",
        subtitle="Ghost Artist",
        image_url=None,
        confidence=Confidence.LOW,
        sources=(SourceRef(provider=ProviderName.LASTFM, external_id="l1", url="https://x/l1"),),
        extras={},
    )
    cache = _FakeContentValidationCache({("lastfm", "l1"): "unfetchable"})
    provider = InMemorySearchProvider(name="deezer", canned=(fetchable,))
    provider2 = InMemorySearchProvider(name="lastfm", canned=(unfetchable,))
    use_case = SearchMusic(
        providers=[provider, provider2],
        history_repo=InMemorySearchHistoryRepository(),
        content_validation_cache=cache,
    )
    output = await use_case.execute(
        SearchMusicInput(
            raw_query="song artist",
            user_id=_USER,
            kinds=frozenset({ResultKind.TRACK}),
        )
    )
    titles = [r.title for r in output.results]
    assert "Good Song" in titles
    assert "Ghost Song" not in titles


# --- _enrich_mbids: MB-source short-circuit (discovery rework follow-up) ---

_CHE_MBID = "0a68f3b5-79c2-4f81-a7bc-ebc977602e86"


def _artist_result(
    title: str,
    sources: tuple[SourceRef, ...],
    extras: dict[str, object] | None = None,
) -> SearchResult:
    return SearchResult(
        kind=ResultKind.ARTIST,
        title=title,
        subtitle=None,
        image_url=None,
        confidence=Confidence.LOW,
        sources=sources,
        extras=extras if extras is not None else {},
    )


@pytest.mark.unit
@pytest.mark.asyncio
async def test_enrich_mbids_short_circuits_on_musicbrainz_source() -> None:
    """An artist carrying a MB SourceRef gets its mbid from that source — no URL lookup."""
    from tests._doubles.spy_mbid_resolver import SpyMbidResolver

    artist = _artist_result(
        "Che",
        sources=(
            SourceRef(
                provider=ProviderName.DEEZER,
                external_id="234701081",
                url="https://www.deezer.com/artist/234701081",
            ),
            SourceRef(
                provider=ProviderName.MUSICBRAINZ,
                external_id=_CHE_MBID,
                url=f"https://musicbrainz.org/artist/{_CHE_MBID}",
            ),
        ),
    )
    provider = InMemorySearchProvider(name="deezer", canned=(artist,))
    resolver = SpyMbidResolver(canned="mbid-from-url-lookup-should-not-be-used")
    use_case = SearchMusic(
        providers=[provider],
        history_repo=InMemorySearchHistoryRepository(),
        mbid_resolver=resolver,
    )

    output = await use_case.execute(
        SearchMusicInput(raw_query="che", user_id=_USER, kinds=frozenset({ResultKind.ARTIST}))
    )

    assert output.results[0].extras.get("mbid") == _CHE_MBID
    assert resolver.calls == []


@pytest.mark.unit
@pytest.mark.asyncio
async def test_enrich_mbids_short_circuit_not_capped_at_three_artists() -> None:
    """The free MB-source path applies to every artist; only URL lookups are capped."""
    from tests._doubles.spy_mbid_resolver import SpyMbidResolver

    artists = tuple(
        _artist_result(
            f"Artist {i}",
            sources=(
                SourceRef(
                    provider=ProviderName.MUSICBRAINZ,
                    external_id=f"mbid-{i}",
                    url=f"https://musicbrainz.org/artist/mbid-{i}",
                ),
            ),
        )
        for i in range(4)
    )
    resolver = SpyMbidResolver()
    use_case = SearchMusic(
        providers=[],
        history_repo=InMemorySearchHistoryRepository(),
        mbid_resolver=resolver,
    )

    enriched = await use_case._enrich_mbids(artists)

    assert [r.extras.get("mbid") for r in enriched] == [f"mbid-{i}" for i in range(4)]
    assert resolver.calls == []


def _deezer_only_artist(i: int) -> SearchResult:
    return _artist_result(
        f"Artist {i}",
        sources=(
            SourceRef(
                provider=ProviderName.DEEZER,
                external_id=f"dz-{i}",
                url=f"https://www.deezer.com/artist/{i}",
            ),
        ),
    )


class _ConcurrencyProbeMbidResolver:
    """Resolver that only completes once two resolve() calls are in flight.

    A sequential caller deadlocks (caught by wait_for); a parallel caller
    releases the barrier and every call returns its mbid.
    """

    def __init__(self) -> None:
        self.in_flight = 0
        self.barrier = asyncio.Event()
        self.calls: list[str] = []

    async def resolve(self, provider_url: str) -> str | None:
        self.calls.append(provider_url)
        self.in_flight += 1
        if self.in_flight >= 2:
            self.barrier.set()
        await self.barrier.wait()
        return f"mbid-for-{provider_url}"


@pytest.mark.unit
@pytest.mark.asyncio
async def test_enrich_mbids_url_lookups_run_concurrently() -> None:
    artists = tuple(_deezer_only_artist(i) for i in range(3))
    resolver = _ConcurrencyProbeMbidResolver()
    use_case = SearchMusic(
        providers=[],
        history_repo=InMemorySearchHistoryRepository(),
        mbid_resolver=resolver,
    )

    enriched = await asyncio.wait_for(use_case._enrich_mbids(artists), timeout=2.0)

    assert len(resolver.calls) == 3
    assert all(r.extras.get("mbid") for r in enriched)


class _OneBadMbidResolver:
    """First call raises; later calls return a canned mbid."""

    def __init__(self) -> None:
        self.calls = 0

    async def resolve(self, provider_url: str) -> str | None:
        self.calls += 1
        if self.calls == 1:
            raise RuntimeError("boom")
        return f"mbid-for-{provider_url}"


@pytest.mark.unit
@pytest.mark.asyncio
async def test_enrich_mbids_swallows_per_lookup_exceptions() -> None:
    artists = tuple(_deezer_only_artist(i) for i in range(3))
    use_case = SearchMusic(
        providers=[],
        history_repo=InMemorySearchHistoryRepository(),
        mbid_resolver=_OneBadMbidResolver(),
    )

    enriched = await use_case._enrich_mbids(artists)

    enriched_count = sum(1 for r in enriched if r.extras.get("mbid"))
    assert enriched_count == 2


# --- _enrich: Genius track-hint orchestration (discovery rework follow-up) ---


class _HintRecordingGeniusResolver:
    """Genius double: records track_hints per call; succeeds only WITH hints."""

    def __init__(self) -> None:
        self.calls: list[tuple[str, tuple[str, ...]]] = []

    async def resolve_artwork(
        self,
        kind: ResultKind,
        title: str,
        subtitle: str | None,
        *,
        track_hints: tuple[str, ...] = (),
    ) -> str | None:
        _ = (kind, subtitle)
        self.calls.append((title, tuple(track_hints)))
        return "https://images.genius.com/che.jpg" if track_hints else None


def _che_with_mb_source() -> SearchResult:
    return _artist_result(
        "Che",
        sources=(
            SourceRef(
                provider=ProviderName.MUSICBRAINZ,
                external_id=_CHE_MBID,
                url=f"https://musicbrainz.org/artist/{_CHE_MBID}",
            ),
        ),
    )


@pytest.mark.unit
@pytest.mark.asyncio
async def test_enrich_retries_genius_with_filtered_track_hints() -> None:
    from tests._doubles.fake_artist_track_title_source import FakeArtistTrackTitleSource

    genius = _HintRecordingGeniusResolver()
    titles = FakeArtistTrackTitleSource(titles=("?????", "#RESIDE", "agenda"))
    provider = InMemorySearchProvider(name="musicbrainz", canned=(_che_with_mb_source(),))
    use_case = SearchMusic(
        providers=[provider],
        history_repo=InMemorySearchHistoryRepository(),
        genius_resolver=genius,
        track_title_source=titles,
    )

    output = await use_case.execute(
        SearchMusicInput(raw_query="che", user_id=_USER, kinds=frozenset({ResultKind.ARTIST}))
    )

    assert titles.calls == [(_CHE_MBID, 10)]
    # First attempt hint-less, second with junk-filtered titles ("?????"  dropped).
    assert genius.calls == [("Che", ()), ("Che", ("#RESIDE", "agenda"))]
    assert output.results[0].image_url == "https://images.genius.com/che.jpg"


@pytest.mark.unit
@pytest.mark.asyncio
async def test_enrich_skips_hint_fetch_without_mbid() -> None:
    from tests._doubles.fake_artist_track_title_source import FakeArtistTrackTitleSource

    genius = _HintRecordingGeniusResolver()
    titles = FakeArtistTrackTitleSource(titles=("agenda",))
    artist = _artist_result(
        "Che",
        sources=(
            SourceRef(
                provider=ProviderName.SOUNDCLOUD,
                external_id="sc-1",
                url="https://soundcloud.com/che",
            ),
        ),
    )
    provider = InMemorySearchProvider(name="soundcloud", canned=(artist,))
    use_case = SearchMusic(
        providers=[provider],
        history_repo=InMemorySearchHistoryRepository(),
        genius_resolver=genius,
        track_title_source=titles,
    )

    await use_case.execute(
        SearchMusicInput(raw_query="che", user_id=_USER, kinds=frozenset({ResultKind.ARTIST}))
    )

    assert titles.calls == []
    assert genius.calls == [("Che", ())]


@pytest.mark.unit
@pytest.mark.asyncio
async def test_enrich_skips_hint_fetch_for_non_artist_results() -> None:
    from tests._doubles.fake_artist_track_title_source import FakeArtistTrackTitleSource

    genius = _HintRecordingGeniusResolver()
    titles = FakeArtistTrackTitleSource(titles=("agenda",))
    track = SearchResult(
        kind=ResultKind.TRACK,
        title="agenda",
        subtitle="Che",
        image_url=None,
        confidence=Confidence.LOW,
        sources=(
            SourceRef(
                provider=ProviderName.MUSICBRAINZ,
                external_id="rec-1",
                url="https://musicbrainz.org/recording/rec-1",
            ),
        ),
        extras={"mbid": "rec-1"},
    )
    provider = InMemorySearchProvider(name="musicbrainz", canned=(track,))
    use_case = SearchMusic(
        providers=[provider],
        history_repo=InMemorySearchHistoryRepository(),
        genius_resolver=genius,
        track_title_source=titles,
    )

    await use_case.execute(
        SearchMusicInput(raw_query="che agenda", user_id=_USER, kinds=frozenset({ResultKind.TRACK}))
    )

    assert titles.calls == []


@pytest.mark.unit
@pytest.mark.asyncio
async def test_enrich_swallows_hint_fetch_failure() -> None:
    from tests._doubles.fake_artist_track_title_source import FakeArtistTrackTitleSource

    genius = _HintRecordingGeniusResolver()
    titles = FakeArtistTrackTitleSource(raises=True)
    provider = InMemorySearchProvider(name="musicbrainz", canned=(_che_with_mb_source(),))
    use_case = SearchMusic(
        providers=[provider],
        history_repo=InMemorySearchHistoryRepository(),
        genius_resolver=genius,
        track_title_source=titles,
    )

    output = await use_case.execute(
        SearchMusicInput(raw_query="che", user_id=_USER, kinds=frozenset({ResultKind.ARTIST}))
    )

    assert genius.calls == [("Che", ())]
    assert output.results[0].image_url is None


# --- _enrich: artwork outcome cache (discovery rework follow-up) ---


@pytest.mark.unit
@pytest.mark.asyncio
async def test_enrich_warm_artwork_cache_skips_resolvers() -> None:
    from tests._doubles.in_memory_artwork_cache import InMemoryArtworkCache

    genius = _HintRecordingGeniusResolver()
    cache = InMemoryArtworkCache()
    cache.seed(ResultKind.ARTIST, "Che", None, _CHE_MBID, "https://cached.example/che.jpg")
    provider = InMemorySearchProvider(name="musicbrainz", canned=(_che_with_mb_source(),))
    use_case = SearchMusic(
        providers=[provider],
        history_repo=InMemorySearchHistoryRepository(),
        genius_resolver=genius,
        artwork_cache=cache,
    )

    output = await use_case.execute(
        SearchMusicInput(raw_query="che", user_id=_USER, kinds=frozenset({ResultKind.ARTIST}))
    )

    assert output.results[0].image_url == "https://cached.example/che.jpg"
    assert genius.calls == []


@pytest.mark.unit
@pytest.mark.asyncio
async def test_enrich_warm_negative_artwork_cache_skips_resolvers() -> None:
    from tests._doubles.in_memory_artwork_cache import InMemoryArtworkCache

    genius = _HintRecordingGeniusResolver()
    cache = InMemoryArtworkCache()
    cache.seed(ResultKind.ARTIST, "Che", None, _CHE_MBID, None)
    provider = InMemorySearchProvider(name="musicbrainz", canned=(_che_with_mb_source(),))
    use_case = SearchMusic(
        providers=[provider],
        history_repo=InMemorySearchHistoryRepository(),
        genius_resolver=genius,
        artwork_cache=cache,
    )

    output = await use_case.execute(
        SearchMusicInput(raw_query="che", user_id=_USER, kinds=frozenset({ResultKind.ARTIST}))
    )

    assert output.results[0].image_url is None
    assert genius.calls == []


@pytest.mark.unit
@pytest.mark.asyncio
async def test_enrich_cold_artwork_cache_stores_waterfall_outcome() -> None:
    from tests._doubles.fake_artist_track_title_source import FakeArtistTrackTitleSource
    from tests._doubles.in_memory_artwork_cache import InMemoryArtworkCache

    genius = _HintRecordingGeniusResolver()
    titles = FakeArtistTrackTitleSource(titles=("agenda",))
    cache = InMemoryArtworkCache()
    provider = InMemorySearchProvider(name="musicbrainz", canned=(_che_with_mb_source(),))
    use_case = SearchMusic(
        providers=[provider],
        history_repo=InMemorySearchHistoryRepository(),
        genius_resolver=genius,
        track_title_source=titles,
        artwork_cache=cache,
    )

    await use_case.execute(
        SearchMusicInput(raw_query="che", user_id=_USER, kinds=frozenset({ResultKind.ARTIST}))
    )

    assert cache.set_calls == [
        (("artist", "Che", "", _CHE_MBID), "https://images.genius.com/che.jpg")
    ]


@pytest.mark.unit
@pytest.mark.asyncio
async def test_enrich_cold_artwork_cache_stores_negative_outcome() -> None:
    from tests._doubles.in_memory_artwork_cache import InMemoryArtworkCache

    genius = _HintRecordingGeniusResolver()  # fails without hints; no title source wired
    cache = InMemoryArtworkCache()
    provider = InMemorySearchProvider(name="musicbrainz", canned=(_che_with_mb_source(),))
    use_case = SearchMusic(
        providers=[provider],
        history_repo=InMemorySearchHistoryRepository(),
        genius_resolver=genius,
        artwork_cache=cache,
    )

    await use_case.execute(
        SearchMusicInput(raw_query="che", user_id=_USER, kinds=frozenset({ResultKind.ARTIST}))
    )

    assert cache.set_calls == [(("artist", "Che", "", _CHE_MBID), None)]
