"""SearchHistoryEntry — per-user search-history row aggregate.

Per AC#11. Identity is a SearchHistoryEntryId (UUID); equality by id
per the entity rule in .claude/rules/domain-layer.md. Ring-buffer trim
+ distinct-recent reads are repository concerns (slice 37).
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime  # noqa: TC003  # used as runtime annotation by dataclass
from uuid import UUID

from altune.domain.shared.user_id import UserId  # noqa: TC001


@dataclass(frozen=True, slots=True)
class SearchHistoryEntryId:
    """A SearchHistoryEntry's stable identity, opaque to the domain."""

    value: UUID

    def __post_init__(self) -> None:
        if not isinstance(self.value, UUID):
            raise TypeError(
                f"SearchHistoryEntryId.value must be a uuid.UUID, got {type(self.value).__name__}"
            )

    def __str__(self) -> str:
        return str(self.value)


@dataclass(frozen=True, slots=True, eq=False)
class SearchHistoryEntry:
    """One persisted search executed by a user."""

    id: SearchHistoryEntryId
    user_id: UserId
    query: str
    query_norm: str
    executed_at: datetime
    result_clicked_signature: str | None

    def __post_init__(self) -> None:
        if not self.query:
            raise ValueError("SearchHistoryEntry.query must be non-empty")
        if not self.query_norm:
            raise ValueError("SearchHistoryEntry.query_norm must be non-empty")

    def __eq__(self, other: object) -> bool:
        if not isinstance(other, SearchHistoryEntry):
            return NotImplemented
        return self.id == other.id

    def __hash__(self) -> int:
        return hash(self.id)
