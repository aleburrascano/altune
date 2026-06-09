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
- year, when present, is positive
- track_number, when present, is positive
- audio_ref set ↔ acquisition_status = READY (bidirectional)
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime  # noqa: TC003  # used as runtime annotation by dataclass

from altune.domain.catalog.acquisition_status import AcquisitionStatus
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
    year: int | None = None
    genre: str | None = None
    track_number: int | None = None
    album_artist: str | None = None
    isrc: str | None = None
    audio_ref: str | None = None
    failure_reason: str | None = None

    def __post_init__(self) -> None:
        if not self.title:
            raise ValueError("Track.title must be non-empty")
        if not self.artist:
            raise ValueError("Track.artist must be non-empty")
        if self.duration_seconds is not None and self.duration_seconds < 0:
            raise ValueError("Track.duration_seconds must be non-negative when present")
        if self.year is not None and self.year <= 0:
            raise ValueError("Track.year must be positive when present")
        if self.track_number is not None and self.track_number <= 0:
            raise ValueError("Track.track_number must be positive when present")
        if self.audio_ref is not None and self.acquisition_status is not AcquisitionStatus.READY:
            raise ValueError("Track.audio_ref requires acquisition_status = READY")
        if self.audio_ref is None and self.acquisition_status is AcquisitionStatus.READY:
            raise ValueError("Track.acquisition_status = READY requires audio_ref to be set")
        if self.acquisition_status is AcquisitionStatus.FAILED and not self.failure_reason:
            raise ValueError("Track.acquisition_status = FAILED requires failure_reason")
        if self.failure_reason and self.acquisition_status is not AcquisitionStatus.FAILED:
            raise ValueError("Track.failure_reason requires acquisition_status = FAILED")

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
