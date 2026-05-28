"""InMemoryQueryCache — Fowler-style fake for unit-testing.

Plain dict backing; TTL not enforced (tests assert expiry by clearing).
"""

from __future__ import annotations

from datetime import UTC, datetime
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from datetime import timedelta

    from altune.domain.discovery.result_kind import ResultKind
    from altune.domain.discovery.search_result import SearchResult


class InMemoryQueryCache:
    """Plain in-process cache; tests control eviction explicitly."""

    def __init__(self) -> None:
        self._entries: dict[
            tuple[str, str, frozenset[ResultKind]],
            tuple[tuple[SearchResult, ...], datetime],
        ] = {}

    async def get(
        self,
        provider: str,
        query_norm: str,
        kinds: frozenset[ResultKind],
    ) -> tuple[tuple[SearchResult, ...], datetime] | None:
        return self._entries.get((provider, query_norm, kinds))

    async def set(
        self,
        provider: str,
        query_norm: str,
        kinds: frozenset[ResultKind],
        results: tuple[SearchResult, ...],
        ttl: timedelta,
    ) -> None:
        _ = ttl  # honored in production adapter; in-memory tests bypass expiry
        self._entries[(provider, query_norm, kinds)] = (results, datetime.now(UTC))

    def clear(self) -> None:
        self._entries.clear()
