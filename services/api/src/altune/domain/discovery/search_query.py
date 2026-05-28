"""SearchQuery — validated user query for the discovery use case.

Per AC#17 (validation). Invariants:
- raw is non-empty (post-trim)
- 1 <= limit <= 50
- kinds is a non-empty frozenset
"""

from __future__ import annotations

from dataclasses import dataclass

from altune.domain.discovery.result_kind import ResultKind  # noqa: TC001


@dataclass(frozen=True, slots=True)
class SearchQuery:
    """User search query after API-boundary validation."""

    raw: str
    query_norm: str
    kinds: frozenset[ResultKind]
    limit: int

    def __post_init__(self) -> None:
        if not self.raw or not self.raw.strip():
            raise ValueError("SearchQuery.raw must be non-empty")
        if not 1 <= self.limit <= 50:
            raise ValueError("SearchQuery.limit must be in [1, 50]")
        if not self.kinds:
            raise ValueError("SearchQuery.kinds must be non-empty")
