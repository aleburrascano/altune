"""SearchClick — per-user click-tracking row aggregate.

Per AC#15 + AC#16. Identity is a SearchClickId (UUID); equality by id.
Sliding-window dedup is a repository concern (slice 40).
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime  # noqa: TC003  # used as runtime annotation by dataclass
from uuid import UUID

from altune.domain.discovery.confidence import Confidence  # noqa: TC001
from altune.domain.shared.user_id import UserId  # noqa: TC001


@dataclass(frozen=True, slots=True)
class SearchClickId:
    """A SearchClick's stable identity, opaque to the domain."""

    value: UUID

    def __post_init__(self) -> None:
        if not isinstance(self.value, UUID):
            raise TypeError(
                f"SearchClickId.value must be a uuid.UUID, got {type(self.value).__name__}"
            )

    def __str__(self) -> str:
        return str(self.value)


@dataclass(frozen=True, slots=True, eq=False)
class SearchClick:
    """One persisted click on a discovery result."""

    id: SearchClickId
    user_id: UserId
    query_norm: str
    result_signature: str
    position: int
    confidence: Confidence
    clicked_at: datetime

    def __post_init__(self) -> None:
        if not self.query_norm:
            raise ValueError("SearchClick.query_norm must be non-empty")
        if not self.result_signature:
            raise ValueError("SearchClick.result_signature must be non-empty")
        if self.position < 0:
            raise ValueError("SearchClick.position must be non-negative")

    def __eq__(self, other: object) -> bool:
        if not isinstance(other, SearchClick):
            return NotImplemented
        return self.id == other.id

    def __hash__(self) -> int:
        return hash(self.id)
