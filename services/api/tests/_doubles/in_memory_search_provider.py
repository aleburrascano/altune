"""InMemorySearchProvider — Fowler-style stub for unit-testing use cases.

Returns canned ProviderSearchResponse without doing any I/O.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import TYPE_CHECKING

from altune.application.discovery.ports import ProviderSearchResponse
from altune.domain.discovery.provider_status import ProviderStatus

if TYPE_CHECKING:
    from altune.domain.discovery.result_kind import ResultKind
    from altune.domain.discovery.search_result import SearchResult


@dataclass
class InMemorySearchProvider:
    """Configurable stub for SearchProvider."""

    name: str  # mirrors the SearchProvider.name property
    canned: tuple[SearchResult, ...] = field(default_factory=tuple)
    status: ProviderStatus = ProviderStatus.OK
    latency_ms: int = 0
    url_lookup: dict[str, SearchResult] = field(default_factory=dict)
    delay_s: float = 0.0  # cooperatively sleep before responding (for parallelism tests)
    raises: BaseException | None = None  # if set, search() raises this

    async def search(
        self,
        query: str,
        kinds: frozenset[ResultKind],
        limit: int,
    ) -> ProviderSearchResponse:
        _ = (query, kinds, limit)
        if self.delay_s > 0:
            import asyncio

            await asyncio.sleep(self.delay_s)
        if self.raises is not None:
            raise self.raises
        return ProviderSearchResponse(
            provider_name=self.name,
            status=self.status,
            results=self.canned if self.status is ProviderStatus.OK else (),
            latency_ms=self.latency_ms,
        )

    async def lookup_by_url(self, url: str) -> SearchResult | None:
        return self.url_lookup.get(url)
