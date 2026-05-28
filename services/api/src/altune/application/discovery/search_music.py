"""SearchMusic — discovery use case.

Slice 16 spine: one provider, no cache, no scatter-gather. Scatter-gather
+ cache + circuit breaker layer on at later slices. Per ADR-0007.
"""

from __future__ import annotations

import asyncio
import logging
import time
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from typing import TYPE_CHECKING
from uuid import uuid4

from altune.application.discovery.circuit_breaker import CircuitBreaker
from altune.application.discovery.dedup import dedup_and_rank
from altune.application.discovery.normalize import normalize_for_match
from altune.domain.discovery.provider_status import ProviderStatus
from altune.domain.discovery.search_history_entry import (
    SearchHistoryEntry,
    SearchHistoryEntryId,
)

if TYPE_CHECKING:
    from collections.abc import Mapping, Sequence

    from altune.application.discovery.ports import (
        QueryCache,
        SearchHistoryRepository,
        SearchProvider,
    )
    from altune.domain.discovery.result_kind import ResultKind
    from altune.domain.discovery.search_result import SearchResult
    from altune.domain.shared.user_id import UserId

_log = logging.getLogger(__name__)

_DEFAULT_PER_SOURCE_TIMEOUT_S = 1.5

# Per-source default TTLs from ADR-0007 §3.4.
_DEFAULT_TTLS = {
    "musicbrainz": timedelta(hours=24),
    "lastfm": timedelta(hours=12),
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
    _breakers: dict[str, CircuitBreaker] = field(default_factory=dict, init=False)

    def _breaker_for(self, provider_name: str) -> CircuitBreaker:
        if provider_name not in self._breakers:
            self._breakers[provider_name] = CircuitBreaker(name=provider_name)
        return self._breakers[provider_name]

    def _ttl_for(self, provider_name: str) -> timedelta:
        return self.cache_ttls.get(provider_name, timedelta(hours=1))

    async def execute(self, request: SearchMusicInput) -> SearchMusicOutput:
        query_norm = normalize_for_match(request.raw_query)

        # Fan out across providers in parallel via asyncio.gather. Each
        # task converts its own exceptions into a ProviderStatusSummary
        # so a single failure can't cancel siblings.
        tasks = [
            self._call_provider_with_cache(provider, request, query_norm)
            for provider in self.providers
        ]
        per_provider = await asyncio.gather(*tasks)
        summaries: list[ProviderStatusSummary] = []
        gathered: list[SearchResult] = []
        cache_hit_fetched_ats: list[datetime] = []
        for summary, results, cache_fetched_at in per_provider:
            summaries.append(summary)
            if summary.status is ProviderStatus.OK:
                gathered.extend(results)
            if cache_fetched_at is not None:
                cache_hit_fetched_ats.append(cache_fetched_at)

        merged = dedup_and_rank(gathered)
        partial = any(s.status is not ProviderStatus.OK for s in summaries)
        cache_hit = bool(cache_hit_fetched_ats)
        cache_fetched_at = min(cache_hit_fetched_ats) if cache_hit_fetched_ats else None

        # Persist history best-effort; never fail the search on persist error.
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
