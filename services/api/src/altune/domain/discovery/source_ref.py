"""SourceRef — per-source reference attached to a SearchResult.

Each merged SearchResult carries a tuple of SourceRefs (one per provider
that returned the same canonical recording). Per AC#1 wire shape.
"""

from __future__ import annotations

from dataclasses import dataclass

from altune.domain.discovery.provider import ProviderName  # noqa: TC001


@dataclass(frozen=True, slots=True)
class SourceRef:
    """One provider's reference to a SearchResult.

    Immutable, attribute-equal. Hashable so SourceRefs can live in sets +
    tuples on the parent SearchResult.
    """

    provider: ProviderName
    external_id: str
    url: str

    def __post_init__(self) -> None:
        if not self.external_id:
            raise ValueError("SourceRef.external_id must be non-empty")
        if not self.url:
            raise ValueError("SourceRef.url must be non-empty")
