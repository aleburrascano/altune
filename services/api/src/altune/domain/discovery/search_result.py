"""SearchResult — merged discovery result, the canonical wire shape.

Per AC#1 + AC#3. One SearchResult may carry multiple SourceRefs when
the dedup engine merges providers' results into one canonical entry
(ISRC match or JW >= 0.85 normalized similarity per slice 14).

Invariants enforced in __post_init__:
- title is non-empty
- sources is a non-empty tuple
"""

from __future__ import annotations

from collections.abc import Mapping  # noqa: TC003  # used as runtime annotation by dataclass
from dataclasses import dataclass

from altune.domain.discovery.confidence import Confidence  # noqa: TC001
from altune.domain.discovery.result_kind import ResultKind  # noqa: TC001
from altune.domain.discovery.source_ref import SourceRef  # noqa: TC001


@dataclass(frozen=True, slots=True)
class SearchResult:
    """One ranked + dedup'd entry in a discovery search response."""

    kind: ResultKind
    title: str
    subtitle: str | None
    image_url: str | None
    confidence: Confidence
    sources: tuple[SourceRef, ...]
    extras: Mapping[str, object]

    def __post_init__(self) -> None:
        if not self.title:
            raise ValueError("SearchResult.title must be non-empty")
        if not self.sources:
            raise ValueError("SearchResult.sources must be a non-empty tuple")
