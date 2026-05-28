"""UserId — typed UUID wrapper used across bounded contexts.

Per ADR-0004, every tenant-scoped use case takes user_id: UserId as a
first-class input so the application layer never sees a bare UUID and the
tenancy seam is explicit in every signature. The runtime authority for the
caller's identity is platform.auth.current_user_id (separate slice); this
type is purely the carrier.

This is one of the few cross-context types. Most domain types live under a
specific bounded context (catalog/, library/, playback/, metadata/).
"""

from __future__ import annotations

from dataclasses import dataclass
from uuid import UUID


@dataclass(frozen=True, slots=True)
class UserId:
    """A user's stable identity, opaque to the domain.

    Equality is by value; instances are immutable.
    """

    value: UUID

    def __post_init__(self) -> None:
        if not isinstance(self.value, UUID):
            raise TypeError(f"UserId.value must be a uuid.UUID, got {type(self.value).__name__}")

    def __str__(self) -> str:
        return str(self.value)
