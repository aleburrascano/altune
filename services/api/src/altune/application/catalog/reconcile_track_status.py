"""ReconcileTrackStatus — mark a track as failed when its audio is missing.

Called by the stream handler on file-not-found and by the CLI health check.
The use case loads the track, verifies the audio is actually missing via the
AudioStore port, transitions the track to FAILED, persists, and returns the
domain event.
"""

from __future__ import annotations

from dataclasses import dataclass, replace
from datetime import UTC, datetime
from typing import TYPE_CHECKING

from altune.domain.catalog.acquisition_status import AcquisitionStatus
from altune.domain.catalog.events import TrackAcquisitionFailed

if TYPE_CHECKING:
    from altune.application.catalog.ports import AudioStore, TrackRepository
    from altune.domain.catalog.track_id import TrackId
    from altune.domain.shared.user_id import UserId


@dataclass(frozen=True, slots=True)
class ReconcileInput:
    track_id: TrackId
    user_id: UserId
    reason: str


@dataclass(frozen=True, slots=True)
class ReconcileOutput:
    reconciled: bool
    event: TrackAcquisitionFailed | None


class ReconcileTrackStatus:
    def __init__(self, tracks: TrackRepository, audio_store: AudioStore) -> None:
        self._tracks = tracks
        self._audio_store = audio_store

    async def execute(self, inp: ReconcileInput) -> ReconcileOutput:
        track = await self._tracks.get_by_id(inp.track_id, inp.user_id)
        if track is None:
            return ReconcileOutput(reconciled=False, event=None)

        if track.acquisition_status is not AcquisitionStatus.READY:
            return ReconcileOutput(reconciled=False, event=None)

        if track.audio_ref and self._audio_store.exists(track.audio_ref):
            return ReconcileOutput(reconciled=False, event=None)

        failed_track = replace(
            track,
            acquisition_status=AcquisitionStatus.FAILED,
            audio_ref=None,
            failure_reason=inp.reason,
        )
        await self._tracks.update(failed_track)

        event = TrackAcquisitionFailed(
            track_id=track.id,
            user_id=track.user_id,
            reason=inp.reason,
            occurred_at=datetime.now(UTC),
        )
        return ReconcileOutput(reconciled=True, event=event)
