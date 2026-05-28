"""SearchMusic — discovery use case.

Slice 16 spine: one provider, no cache, no scatter-gather. Scatter-gather
+ cache + circuit breaker layer on at later slices. Per ADR-0007.
"""

from __future__ import annotations

import asyncio
import logging
import time
from dataclasses import dataclass, field
from datetime import UTC, datetime
from typing import TYPE_CHECKING
from uuid import uuid4

from altune.application.discovery.dedup import dedup_and_rank
from altune.application.discovery.normalize import normalize_for_match
from altune.domain.discovery.provider_status import ProviderStatus
from altune.domain.discovery.search_history_entry import (
    SearchHistoryEntry,
    SearchHistoryEntryId,
)

if TYPE_CHECKING:
    from collections.abc import Sequence

    from altune.application.discovery.ports import (
        SearchHistoryRepository,
        SearchProvider,
    )
    from altune.domain.discovery.result_kind import ResultKind
    from altune.domain.discovery.search_result import SearchResult
    from altune.domain.shared.user_id import UserId

_log = logging.getLogger(__name__)

_DEFAULT_PER_SOURCE_TIMEOUT_S = 1.5


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
    """Use case: fan out to providers (one v1 spine), dedup + rank, persist history."""

    providers: Sequence[SearchProvider]
    history_repo: SearchHistoryRepository
    history_config: HistoryPersistRingBufferConfig = field(
        default_factory=HistoryPersistRingBufferConfig
    )
    per_source_timeout_s: float = _DEFAULT_PER_SOURCE_TIMEOUT_S

    async def execute(self, request: SearchMusicInput) -> SearchMusicOutput:
        query_norm = normalize_for_match(request.raw_query)
        summaries: list[ProviderStatusSummary] = []
        gathered: list[SearchResult] = []
        for provider in self.providers:
            start = time.perf_counter()
            try:
                resp = await asyncio.wait_for(
                    provider.search(request.raw_query, request.kinds, request.limit),
                    timeout=self.per_source_timeout_s,
                )
                latency_ms = int((time.perf_counter() - start) * 1000)
                summaries.append(
                    ProviderStatusSummary(
                        provider_name=resp.provider_name,
                        status=resp.status,
                        result_count=len(resp.results),
                        latency_ms=latency_ms,
                    )
                )
                if resp.status is ProviderStatus.OK:
                    gathered.extend(resp.results)
            except TimeoutError:
                latency_ms = int((time.perf_counter() - start) * 1000)
                summaries.append(
                    ProviderStatusSummary(
                        provider_name=provider.name,
                        status=ProviderStatus.TIMEOUT,
                        result_count=0,
                        latency_ms=latency_ms,
                    )
                )
            except Exception:
                latency_ms = int((time.perf_counter() - start) * 1000)
                _log.exception("provider %s raised during search", provider.name)
                summaries.append(
                    ProviderStatusSummary(
                        provider_name=provider.name,
                        status=ProviderStatus.ERROR,
                        result_count=0,
                        latency_ms=latency_ms,
                    )
                )

        merged = dedup_and_rank(gathered)
        partial = any(s.status is not ProviderStatus.OK for s in summaries)

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
        )
