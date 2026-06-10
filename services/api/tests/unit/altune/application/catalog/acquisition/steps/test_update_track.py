"""UpdateTrackStep tests."""

from __future__ import annotations

from datetime import UTC, datetime
from uuid import UUID

import pytest
from tests._doubles.in_memory_track_repository import InMemoryTrackRepository

from altune.application.catalog.acquisition.context import AcquisitionContext
from altune.application.catalog.acquisition.steps.update_track import UpdateTrackStep
from altune.domain.catalog.acquisition_status import AcquisitionStatus
from altune.domain.catalog.track import Track
from altune.domain.catalog.track_id import TrackId
from altune.domain.shared.user_id import UserId

_TID = TrackId(UUID("11111111-1111-1111-1111-111111111111"))
_UID = UserId(UUID("00000000-0000-0000-0000-000000000001"))


def _pending_track() -> Track:
    return Track(
        id=_TID,
        user_id=_UID,
        title="Song",
        artist="Artist",
        album=None,
        duration_seconds=200,
        added_at=datetime(2026, 1, 1, tzinfo=UTC),
    )


@pytest.mark.unit
async def test_update_track_step_transitions_to_ready() -> None:
    track = _pending_track()
    repo = InMemoryTrackRepository([track])
    step = UpdateTrackStep(repo)
    ctx = AcquisitionContext(track=track, audio_ref="user/artist/album/song.mp3")

    result = await step.execute(ctx)

    assert result.track is not None
    assert result.track.acquisition_status is AcquisitionStatus.READY
    assert result.track.audio_ref == "user/artist/album/song.mp3"
    stored = await repo.get_by_id(_TID, _UID)
    assert stored is not None
    assert stored.acquisition_status is AcquisitionStatus.READY


@pytest.mark.unit
async def test_update_track_step_rollback_reverts_to_pending() -> None:
    track = _pending_track()
    repo = InMemoryTrackRepository([track])
    step = UpdateTrackStep(repo)
    ctx = AcquisitionContext(track=track, audio_ref="user/artist/album/song.mp3")

    await step.execute(ctx)
    assert ctx.track.acquisition_status is AcquisitionStatus.READY

    await step.rollback(ctx)

    assert ctx.track.acquisition_status is AcquisitionStatus.PENDING
    stored = await repo.get_by_id(_TID, _UID)
    assert stored is not None
    assert stored.acquisition_status is AcquisitionStatus.PENDING
