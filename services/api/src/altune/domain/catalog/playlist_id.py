"""PlaylistId — typed UUID wrapper for a Playlist aggregate's identity."""

from __future__ import annotations

from dataclasses import dataclass
from uuid import UUID


@dataclass(frozen=True, slots=True)
class PlaylistId:
    value: UUID

    def __post_init__(self) -> None:
        if not isinstance(self.value, UUID):
            raise TypeError(
                f"PlaylistId.value must be a uuid.UUID, got {type(self.value).__name__}"
            )

    def __str__(self) -> str:
        return str(self.value)
