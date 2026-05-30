"""Track — aggregate root of the catalog bounded context.

A single audio recording with metadata (title, artist, optional album, optional
duration). Identity is a TrackId; the entity is owned by a user (UserId).

Equality is by identity (Track id) per domain-layer.md's entity rule, not by
attribute. Two Track instances with the same TrackId are the same Track from
the domain's perspective, even if their other attributes differ — this matters
when comparing an in-memory edit with the persisted version.

Invariants enforced in __post_init__:
- title is non-empty
- artist is non-empty
- duration_seconds, when present, is non-negative
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime  # noqa: TC003  # used as runtime annotation by dataclass

from altune.domain.catalog.acquisition_status import AcquisitionStatus  # noqa: TC001
from altune.domain.catalog.track_id import TrackId  # noqa: TC001
from altune.domain.shared.user_id import UserId  # noqa: TC001


@dataclass(frozen=True, slots=True, eq=False)
class Track:
    id: TrackId
    user_id: UserId
    title: str
    artist: str
    album: str | None
    duration_seconds: int | None
    added_at: datetime
    artwork_url: str | None = None
    acquisition_status: AcquisitionStatus = AcquisitionStatus.PENDING

    def __post_init__(self) -> None:
        if not self.title:
            raise ValueError("Track.title must be non-empty")
        if not self.artist:
            raise ValueError("Track.artist must be non-empty")
        if self.duration_seconds is not None and self.duration_seconds < 0:
            raise ValueError("Track.duration_seconds must be non-negative when present")

    def __eq__(self, other: object) -> bool:
        # AIDEV-NOTE: id-based equality is the entity rule from
        # .claude/rules/domain-layer.md. Attribute-based equality would
        # confuse "is this the same track?" with "are these tracks
        # identical right now?" — the former is the load-bearing question.
        if not isinstance(other, Track):
            return NotImplemented
        return self.id == other.id

    def __hash__(self) -> int:
        return hash(self.id)
