"""TrackId — typed UUID wrapper for a Track aggregate's identity.

Mirrors the shape of domain.shared.user_id.UserId. Lives under catalog/
because Track is a catalog aggregate; TrackId only ever identifies a Track.
"""

from __future__ import annotations

from dataclasses import dataclass
from uuid import UUID


@dataclass(frozen=True, slots=True)
class TrackId:
    """A Track's stable identity, opaque to the domain.

    Equality is by value; instances are immutable.
    """

    value: UUID

    def __post_init__(self) -> None:
        if not isinstance(self.value, UUID):
            raise TypeError(
                f"TrackId.value must be a uuid.UUID, got {type(self.value).__name__}"
            )

    def __str__(self) -> str:
        return str(self.value)
