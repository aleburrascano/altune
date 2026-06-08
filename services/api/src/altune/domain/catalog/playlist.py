"""Playlist — aggregate root for user-created track collections.

A named, ordered list of tracks owned by a user. Tracks are referenced by
TrackId (not embedded). Position is 0-indexed and contiguous.

Invariants:
- name is non-empty and at most 100 characters
- positions are contiguous (0..N-1) with no gaps or duplicates
- no duplicate track_ids within one playlist
"""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime  # noqa: TC003

from altune.domain.catalog.playlist_id import PlaylistId  # noqa: TC001
from altune.domain.catalog.track_id import TrackId  # noqa: TC001
from altune.domain.shared.user_id import UserId  # noqa: TC001

MAX_NAME_LENGTH = 100


@dataclass(frozen=True, slots=True)
class PlaylistTrack:
    track_id: TrackId
    position: int

    def __post_init__(self) -> None:
        if self.position < 0:
            raise ValueError("PlaylistTrack.position must be non-negative")


@dataclass(frozen=True, slots=True, eq=False)
class Playlist:
    id: PlaylistId
    user_id: UserId
    name: str
    created_at: datetime
    updated_at: datetime
    tracks: tuple[PlaylistTrack, ...] = field(default_factory=tuple)

    def __post_init__(self) -> None:
        if not self.name:
            raise ValueError("Playlist.name must be non-empty")
        if len(self.name) > MAX_NAME_LENGTH:
            raise ValueError(f"Playlist.name must be at most {MAX_NAME_LENGTH} characters")
        self._validate_positions()

    def _validate_positions(self) -> None:
        if not self.tracks:
            return
        positions = [t.position for t in self.tracks]
        track_ids = [t.track_id for t in self.tracks]
        if len(set(track_ids)) != len(track_ids):
            raise ValueError("Playlist contains duplicate track_ids")
        expected = list(range(len(self.tracks)))
        if sorted(positions) != expected:
            raise ValueError("Playlist track positions must be contiguous 0..N-1")

    def __eq__(self, other: object) -> bool:
        if not isinstance(other, Playlist):
            return NotImplemented
        return self.id == other.id

    def __hash__(self) -> int:
        return hash(self.id)
