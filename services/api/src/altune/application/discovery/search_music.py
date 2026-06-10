"""SearchMusic — discovery use case.

Slice 16 spine: one provider, no cache, no scatter-gather. Scatter-gather
+ cache + circuit breaker layer on at later slices. Per ADR-0007.
"""

from __future__ import annotations

import asyncio
import logging
import time
from dataclasses import dataclass, field, replace
from datetime import UTC, datetime, timedelta
from typing import TYPE_CHECKING
from uuid import uuid4

from altune.application.discovery.circuit_breaker import CircuitBreaker
from altune.application.discovery.dedup import fuse_and_rank, rerank
from altune.application.discovery.normalize import normalize_for_match
from altune.application.discovery.url_router import match_provider
from altune.domain.discovery.provider import ProviderName
from altune.domain.discovery.provider_status import ProviderStatus
from altune.domain.discovery.result_kind import ResultKind
from altune.domain.discovery.search_history_entry import (
    SearchHistoryEntry,
    SearchHistoryEntryId,
)

if TYPE_CHECKING:
    from collections.abc import Callable, Mapping, Sequence

    from altune.application.discovery.ports import (
        ArtistTrackTitleSource,
        ArtworkResolver,
        ContentValidationCache,
        HintedArtworkResolver,
        MbidArtworkResolver,
        MbidResolver,
        PopularityResolver,
        QueryCache,
        SearchHistoryRepository,
        SearchProvider,
    )
    from altune.domain.discovery.quality_score import QualityScore
    from altune.domain.discovery.search_result import SearchResult

    QualityScorer = Callable[[SearchResult], QualityScore]
    from altune.domain.shared.user_id import UserId

_log = logging.getLogger(__name__)

_DEFAULT_PER_SOURCE_TIMEOUT_S = 1.5

# Bound enrichment (popularity + artwork) to the results we're likely to show,
# and cap concurrency so we stay within provider rate limits.
_ENRICH_LIMIT = 25
_ENRICH_CONCURRENCY = 8

# Per-source default TTLs from ADR-0007 §3.4.
_DEFAULT_TTLS = {
    "musicbrainz": timedelta(hours=24),
    "lastfm": timedelta(hours=12),
    "itunes": timedelta(hours=12),
    "deezer": timedelta(hours=6),
    "soundcloud": timedelta(hours=1),
}


@dataclass(frozen=True, slots=True)
class ProviderStatusSummary:
    """Per-provider status info surfaced on the response."""

    provider_name: str
    status: ProviderStatus
    result_count: int
    latency_ms: int


@dataclass(frozen=True, slots=True)
class SearchMusicInput:
    """Input DTO for the use case."""

    raw_query: str
    user_id: UserId
    kinds: frozenset[ResultKind]
    limit: int = 25
    save_history: bool = True


@dataclass(frozen=True, slots=True)
class SearchMusicOutput:
    """Output DTO carrying ranked merged results + per-provider statuses."""

    query: str
    query_norm: str
    results: tuple[SearchResult, ...]
    providers: tuple[ProviderStatusSummary, ...]
    partial: bool
    cache_hit: bool = False
    cache_fetched_at: datetime | None = None


@dataclass(frozen=True, slots=True)
class HistoryPersistRingBufferConfig:
    """Configuration for the ring-buffer trim called after each insert."""

    keep_n: int = 50


@dataclass
class SearchMusic:
    """Use case: fan out to providers, dedup + rank, persist history.

    A per-provider CircuitBreaker is built lazily on first use (keyed by
    provider.name) and persists across requests for the lifetime of the
    SearchMusic instance. Wiring keeps the same SearchMusic instance live
    across requests (constructed once in the lifespan) so breakers don't
    reset every request.
    """

    providers: Sequence[SearchProvider]
    history_repo: SearchHistoryRepository
    history_config: HistoryPersistRingBufferConfig = field(
        default_factory=HistoryPersistRingBufferConfig
    )
    per_source_timeout_s: float = _DEFAULT_PER_SOURCE_TIMEOUT_S
    cache: QueryCache | None = None
    cache_ttls: Mapping[str, timedelta] = field(default_factory=lambda: dict(_DEFAULT_TTLS))
    artwork_resolver: ArtworkResolver | None = None
    popularity_resolver: PopularityResolver | None = None
    quality_scorer: QualityScorer | None = None
    content_validation_cache: ContentValidationCache | None = None
    mbid_resolver: MbidResolver | None = None
    fanart_resolver: MbidArtworkResolver | None = None
    genius_resolver: HintedArtworkResolver | None = None
    track_title_source: ArtistTrackTitleSource | None = None
    _breakers: dict[str, CircuitBreaker] = field(default_factory=dict, init=False)

    def _breaker_for(self, provider_name: str) -> CircuitBreaker:
        if provider_name not in self._breakers:
            self._breakers[provider_name] = CircuitBreaker(name=provider_name)
        return self._breakers[provider_name]

    def _ttl_for(self, provider_name: str) -> timedelta:
        return self.cache_ttls.get(provider_name, timedelta(hours=1))

    async def _filter_unfetchable(
        self, results: tuple[SearchResult, ...]
    ) -> tuple[SearchResult, ...]:
        """Remove results whose every source has a cached UNFETCHABLE status."""
        cache = self.content_validation_cache
        if cache is None:
            return results
        from altune.domain.discovery.content_validation_status import ContentValidationStatus

        kept: list[SearchResult] = []
        for result in results:
            all_unfetchable = True
            for src in result.sources:
                provider_name = (
                    src.provider.value if hasattr(src.provider, "value") else str(src.provider)
                )
                status = await cache.get(provider_name, src.external_id)
                if status is not ContentValidationStatus.UNFETCHABLE:
                    all_unfetchable = False
                    break
            if not all_unfetchable:
                kept.append(result)
        return tuple(kept)

    async def _enrich_mbids(self, results: tuple[SearchResult, ...]) -> tuple[SearchResult, ...]:
        """Backfill extras["mbid"] for artist results that lack one.

        Free path first: an artist carrying a MusicBrainz SourceRef already
        has its MBID as that source's external_id — no network call. Only
        artists without an MB source fall back to the MB URL-lookup resolver
        (one live call per artist, max 3, Deezer URLs only — MB has the best
        Deezer link coverage).
        """
        resolver = self.mbid_resolver
        max_lookups = 3
        enriched: list[SearchResult] = list(results)
        candidates: list[tuple[int, str]] = []
        for i, result in enumerate(results):
            if result.extras.get("mbid"):
                continue
            if result.kind is not ResultKind.ARTIST:
                continue
            mb_src = next(
                (s for s in result.sources if s.provider is ProviderName.MUSICBRAINZ), None
            )
            if mb_src is not None:
                enriched[i] = replace(result, extras={**result.extras, "mbid": mb_src.external_id})
                continue
            if resolver is None or len(candidates) >= max_lookups:
                continue
            deezer_src = next((s for s in result.sources if "deezer.com" in s.url), None)
            if deezer_src is None:
                continue
            candidates.append((i, deezer_src.url))
        if resolver is not None and candidates:

            async def _resolve_one(url: str) -> str | None:
                try:
                    return await resolver.resolve(url)
                except Exception:
                    _log.warning("mbid_lookup_failed url=%s", url, exc_info=True)
                    return None

            mbids = await asyncio.gather(*(_resolve_one(url) for _, url in candidates))
            for (i, _), mbid in zip(candidates, mbids, strict=True):
                if mbid:
                    enriched[i] = replace(
                        enriched[i], extras={**enriched[i].extras, "mbid": mbid}
                    )
        return tuple(enriched)

    async def _enrich(self, results: tuple[SearchResult, ...]) -> tuple[SearchResult, ...]:
        """Bounded, concurrency-capped, best-effort enrichment of the top results.

        Back-fills a UNIFORM popularity (Last.fm getInfo, keyed by artist+title)
        onto every top result regardless of which provider surfaced it, plus a
        cover for art-less results. Never fails the search — resolver errors are
        swallowed and the result is left unchanged.
        """
        pop_resolver = self.popularity_resolver
        art_resolver = self.artwork_resolver
        if (
            pop_resolver is None
            and art_resolver is None
            and self.fanart_resolver is None
            and self.genius_resolver is None
        ):
            return results
        top = results[:_ENRICH_LIMIT]
        if not top:
            return results
        sem = asyncio.Semaphore(_ENRICH_CONCURRENCY)

        async def _one(result: SearchResult) -> SearchResult:
            async with sem:
                extras = dict(result.extras)
                image_url = result.image_url
                changed = False
                if pop_resolver is not None:
                    try:
                        pop = await pop_resolver.resolve_popularity(
                            result.kind, result.title, result.subtitle
                        )
                    except Exception:
                        pop = None
                    if pop is not None:
                        # Uniform Last.fm scale replaces the native popularity so
                        # all results are comparable on the same basis.
                        extras["popularity"] = pop
                        changed = True
                _EMPTY_ART_HASH = "d41d8cd98f00b204e9800998ecf8427e"
                needs_art = result.image_url is None or _EMPTY_ART_HASH in (result.image_url or "")
                try_fanart = needs_art or result.kind is ResultKind.ARTIST
                if try_fanart and self.fanart_resolver is not None:
                    mbid = extras.get("mbid") or result.extras.get("mbid")
                    if isinstance(mbid, str) and mbid:
                        try:
                            url = await self.fanart_resolver.resolve_artwork(
                                result.kind, result.title, result.subtitle, mbid=mbid
                            )
                        except Exception:
                            url = None
                        if url:
                            image_url = url
                            needs_art = False
                            changed = True
                if needs_art and self.genius_resolver is not None:
                    genius = self.genius_resolver
                    try:
                        url = await genius.resolve_artwork(
                            result.kind, result.title, result.subtitle
                        )
                    except Exception:
                        url = None
                    if url is None and result.kind is ResultKind.ARTIST:
                        # Bare name search fails for short/common names — retry
                        # with known-track titles pinned to the artist's MBID.
                        hint_mbid = extras.get("mbid") or result.extras.get("mbid")
                        if isinstance(hint_mbid, str) and hint_mbid:
                            hints = await self._track_hints_for(hint_mbid)
                            if hints:
                                try:
                                    url = await genius.resolve_artwork(
                                        result.kind,
                                        result.title,
                                        result.subtitle,
                                        track_hints=hints,
                                    )
                                except Exception:
                                    url = None
                    if url:
                        image_url = url
                        needs_art = False
                        changed = True
                is_ambiguous_artist = (
                    result.kind is ResultKind.ARTIST and len(result.title.split()) <= 1
                )
                if art_resolver is not None and needs_art and not is_ambiguous_artist:
                    try:
                        url = await art_resolver.resolve_artwork(
                            result.kind, result.title, result.subtitle
                        )
                    except Exception:
                        url = None
                    if url:
                        image_url = url
                        changed = True
                return replace(result, extras=extras, image_url=image_url) if changed else result

        enriched = await asyncio.gather(*(_one(r) for r in top))
        return (*enriched, *results[_ENRICH_LIMIT:])

    async def _track_hints_for(self, mbid: str) -> tuple[str, ...]:
        """Top-track titles for the Genius hint retry; junk titles dropped."""
        source = self.track_title_source
        if source is None:
            return ()
        try:
            response = await source.get_artist_top_tracks(mbid, 10)
        except Exception:
            _log.warning("track_hint_fetch_failed mbid=%s", mbid, exc_info=True)
            return ()
        titles = [r.title for r in response.items if any(c.isalnum() for c in r.title)]
        return tuple(titles[:3])

    async def execute(self, request: SearchMusicInput) -> SearchMusicOutput:
        query_norm = normalize_for_match(request.raw_query)

        # URL-paste short-circuit (AC#10): if the query is a supported
        # provider URL, route to that provider's lookup_by_url. Unsupported
        # URLs (AC#10a) fall through to text scatter-gather below.
        url_provider = match_provider(request.raw_query)
        if url_provider is not None:
            return await self._execute_url_lookup(request, query_norm, url_provider)

        # Fan out across providers in parallel via asyncio.gather. Each
        # task converts its own exceptions into a ProviderStatusSummary
        # so a single failure can't cancel siblings.
        tasks = [
            self._call_provider_with_cache(provider, request, query_norm)
            for provider in self.providers
        ]
        per_provider = await asyncio.gather(*tasks)
        summaries: list[ProviderStatusSummary] = []
        # Keep each provider's results as its own group, in that provider's
        # native relevance order, so fuse_and_rank can apply RRF across lists.
        groups: list[tuple[SearchResult, ...]] = []
        cache_hit_fetched_ats: list[datetime] = []
        for summary, results, cache_fetched_at in per_provider:
            summaries.append(summary)
            if summary.status is ProviderStatus.OK:
                groups.append(results)
            if cache_fetched_at is not None:
                cache_hit_fetched_ats.append(cache_fetched_at)

        merged = fuse_and_rank(groups, query_norm, quality_scorer=self.quality_scorer)
        merged = await self._enrich_mbids(merged)
        merged = await self._filter_unfetchable(merged)
        merged = await self._enrich(merged)
        # Enrichment changed popularity (a sort key), so re-rank when it ran.
        if self.popularity_resolver is not None:
            merged = rerank(merged, query_norm)
        partial = any(s.status is not ProviderStatus.OK for s in summaries)
        cache_hit = bool(cache_hit_fetched_ats)
        cache_fetched_at = min(cache_hit_fetched_ats) if cache_hit_fetched_ats else None

        # Persist history best-effort; skip for debounced as-you-type queries.
        if request.save_history:
            try:
                entry = SearchHistoryEntry(
                    id=SearchHistoryEntryId(uuid4()),
                    user_id=request.user_id,
                    query=request.raw_query,
                    query_norm=query_norm,
                    executed_at=datetime.now(UTC),
                    result_clicked_signature=None,
                )
                await self.history_repo.insert(entry)
                await self.history_repo.trim_to_n(request.user_id, self.history_config.keep_n)
            except Exception:
                _log.exception(
                    "search_history_persist_failed user=%s query_norm=%s",
                    request.user_id,
                    query_norm,
                )

        return SearchMusicOutput(
            query=request.raw_query,
            query_norm=query_norm,
            results=merged,
            providers=tuple(summaries),
            partial=partial,
            cache_hit=cache_hit,
            cache_fetched_at=cache_fetched_at,
        )

    async def _execute_url_lookup(
        self,
        request: SearchMusicInput,
        query_norm: str,
        url_provider: ProviderName,
    ) -> SearchMusicOutput:
        """Short-circuit URL-paste path: call one provider's lookup_by_url."""
        target = next((p for p in self.providers if p.name == url_provider.value), None)
        result: SearchResult | None = None
        if target is not None:
            try:
                result = await target.lookup_by_url(request.raw_query)
            except Exception:
                _log.exception("provider %s lookup_by_url raised", target.name)
                result = None
        if request.save_history:
            await self._persist_history(request, query_norm)
        return SearchMusicOutput(
            query=request.raw_query,
            query_norm=query_norm,
            results=(result,) if result is not None else (),
            providers=(),
            partial=False,
        )

    async def _persist_history(self, request: SearchMusicInput, query_norm: str) -> None:
        """Best-effort history persist; failures are logged + swallowed."""
        try:
            entry = SearchHistoryEntry(
                id=SearchHistoryEntryId(uuid4()),
                user_id=request.user_id,
                query=request.raw_query,
                query_norm=query_norm,
                executed_at=datetime.now(UTC),
                result_clicked_signature=None,
            )
            await self.history_repo.insert(entry)
            await self.history_repo.trim_to_n(request.user_id, self.history_config.keep_n)
        except Exception:
            _log.exception(
                "search_history_persist_failed user=%s query_norm=%s",
                request.user_id,
                query_norm,
            )

    async def _call_provider_with_cache(
        self,
        provider: SearchProvider,
        request: SearchMusicInput,
        query_norm: str,
    ) -> tuple[ProviderStatusSummary, tuple[SearchResult, ...], datetime | None]:
        """Cache-check first; fall through to live call on miss.

        Returns (summary, results, cache_fetched_at). cache_fetched_at is
        non-None iff this provider served from cache.
        """
        if self.cache is not None:
            try:
                cached = await self.cache.get(provider.name, query_norm, request.kinds)
            except Exception:
                _log.warning(
                    "cache_unavailable provider=%s — falling through to live",
                    provider.name,
                    exc_info=True,
                )
                cached = None
            if cached is not None:
                results, fetched_at = cached
                return (
                    ProviderStatusSummary(
                        provider_name=provider.name,
                        status=ProviderStatus.OK,
                        result_count=len(results),
                        latency_ms=0,
                    ),
                    results,
                    fetched_at,
                )
        summary, results = await self._call_provider(provider, request)
        # Write to cache only on OK live calls.
        if self.cache is not None and summary.status is ProviderStatus.OK and results:
            try:
                await self.cache.set(
                    provider.name,
                    query_norm,
                    request.kinds,
                    results,
                    self._ttl_for(provider.name),
                )
            except Exception:
                _log.warning(
                    "cache_unavailable op=set provider=%s",
                    provider.name,
                    exc_info=True,
                )
        return summary, results, None

    async def _call_provider(
        self,
        provider: SearchProvider,
        request: SearchMusicInput,
    ) -> tuple[ProviderStatusSummary, tuple[SearchResult, ...]]:
        """Call one provider; convert exceptions to status — never raises."""
        breaker = self._breaker_for(provider.name)
        if not breaker.should_call():
            return (
                ProviderStatusSummary(
                    provider_name=provider.name,
                    status=ProviderStatus.CIRCUIT_OPEN,
                    result_count=0,
                    latency_ms=0,
                ),
                (),
            )
        start = time.perf_counter()
        try:
            resp = await asyncio.wait_for(
                provider.search(request.raw_query, request.kinds, request.limit),
                timeout=self.per_source_timeout_s,
            )
            latency_ms = int((time.perf_counter() - start) * 1000)
            # OK -> success; ERROR -> failure; RATE_LIMITED -> ignored.
            if resp.status is ProviderStatus.OK:
                breaker.record_success()
            elif resp.status is ProviderStatus.ERROR:
                breaker.record_failure()
            summary = ProviderStatusSummary(
                provider_name=resp.provider_name,
                status=resp.status,
                result_count=len(resp.results),
                latency_ms=latency_ms,
            )
            results = resp.results if resp.status is ProviderStatus.OK else ()
            return summary, results
        except TimeoutError:
            latency_ms = int((time.perf_counter() - start) * 1000)
            breaker.record_failure()
            return (
                ProviderStatusSummary(
                    provider_name=provider.name,
                    status=ProviderStatus.TIMEOUT,
                    result_count=0,
                    latency_ms=latency_ms,
                ),
                (),
            )
        except Exception:
            latency_ms = int((time.perf_counter() - start) * 1000)
            _log.exception("provider %s raised during search", provider.name)
            breaker.record_failure()
            return (
                ProviderStatusSummary(
                    provider_name=provider.name,
                    status=ProviderStatus.ERROR,
                    result_count=0,
                    latency_ms=latency_ms,
                ),
                (),
            )
