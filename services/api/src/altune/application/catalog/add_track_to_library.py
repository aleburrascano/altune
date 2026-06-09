"""Add a discovered track to the user's library (metadata only).

Write use case behind POST /v1/tracks. Builds a Track from the save request,
persists it via the repository (which dedups on the natural key), and emits a
TrackAddedToLibrary domain event to the logger on a fresh save only. Audio
acquisition is a later spec; the track starts at AcquisitionStatus.PENDING.

The Track aggregate enforces its own invariants (non-empty title/artist) at
construction, so an invalid save raises before any persistence happens.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import UTC, datetime
from typing import TYPE_CHECKING
from uuid import uuid4

import structlog

from altune.domain.catalog.events import TrackAddedToLibrary
from altune.domain.catalog.track import Track
from altune.domain.catalog.track_id import TrackId

if TYPE_CHECKING:
    from altune.application.catalog.ports import TrackRepository
    from altune.domain.shared.user_id import UserId

_logger = structlog.get_logger(__name__)


@dataclass(frozen=True)
class AddTrackToLibraryInput:
    user_id: UserId
    title: str
    artist: str
    album: str | None
    duration_seconds: int | None
    artwork_url: str | None
    isrc: str | None = None
    year: int | None = None
    genre: str | None = None
    album_artist: str | None = None


@dataclass(frozen=True)
class AddTrackToLibraryOutput:
    track: Track
    created: bool


class AddTrackToLibrary:
    def __init__(self, tracks: TrackRepository) -> None:
        self._tracks = tracks

    async def execute(self, input: AddTrackToLibraryInput) -> AddTrackToLibraryOutput:
        track = Track(
            id=TrackId(uuid4()),
            user_id=input.user_id,
            title=input.title,
            artist=input.artist,
            album=input.album,
            duration_seconds=input.duration_seconds,
            added_at=datetime.now(UTC),
            artwork_url=input.artwork_url,
            isrc=input.isrc,
            year=input.year,
            genre=input.genre,
            album_artist=input.album_artist,
        )
        persisted, created = await self._tracks.add(track)
        if created:
            event = TrackAddedToLibrary(
                track_id=persisted.id,
                user_id=persisted.user_id,
                occurred_at=datetime.now(UTC),
            )
            _logger.info(
                "track_added_to_library",
                track_id=str(event.track_id),
                user_id=str(event.user_id),
            )
        return AddTrackToLibraryOutput(track=persisted, created=created)
