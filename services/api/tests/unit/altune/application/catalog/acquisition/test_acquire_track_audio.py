"""AcquireTrackAudio use case — full flow with fakes."""

from __future__ import annotations

from datetime import UTC, datetime
from uuid import UUID

import pytest
from tests._doubles.fake_audio_searcher import FakeAudioSearcher
from tests._doubles.fake_audio_store import FakeAudioStore
from tests._doubles.in_memory_track_repository import InMemoryTrackRepository

from altune.application.catalog.acquisition.acquire_track_audio import AcquireTrackAudio
from altune.application.catalog.ports import AudioCandidate
from altune.domain.catalog.acquisition_status import AcquisitionStatus
from altune.domain.catalog.track import Track
from altune.domain.catalog.track_id import TrackId
from altune.domain.shared.user_id import UserId

_TID = TrackId(UUID("11111111-1111-1111-1111-111111111111"))
_UID = UserId(UUID("00000000-0000-0000-0000-000000000001"))

_GOOD_CANDIDATE = AudioCandidate(
    title="Blinding Lights",
    artist="The Weeknd",
    duration_seconds=200,
    url="http://yt/blinding-lights",
)


def _pending_track(*, isrc: str | None = None) -> Track:
    return Track(
        id=_TID, user_id=_UID, title="Blinding Lights", artist="The Weeknd",
        album="After Hours", duration_seconds=200,
        added_at=datetime(2026, 1, 1, tzinfo=UTC), isrc=isrc,
    )


def _ready_track() -> Track:
    return Track(
        id=_TID, user_id=_UID, title="Blinding Lights", artist="The Weeknd",
        album="After Hours", duration_seconds=200,
        added_at=datetime(2026, 1, 1, tzinfo=UTC),
        audio_ref="user/artist/album/song.mp3",
        acquisition_status=AcquisitionStatus.READY,
    )


@pytest.mark.unit
async def test_acquire_transitions_pending_to_ready() -> None:
    track = _pending_track()
    repo = InMemoryTrackRepository([track])
    searcher = FakeAudioSearcher({
        "ytmsearch:Blinding Lights The Weeknd": [_GOOD_CANDIDATE],
    })
    store = FakeAudioStore()
    use_case = AcquireTrackAudio(repo, searcher, store)

    await use_case.execute(_TID, _UID)

    result = await repo.get_by_id(_TID, _UID)
    assert result is not None
    assert result.acquisition_status is AcquisitionStatus.READY
    assert result.audio_ref is not None
    assert len(store.stored) == 1


@pytest.mark.unit
async def test_acquire_sets_failed_on_no_match() -> None:
    track = _pending_track()
    repo = InMemoryTrackRepository([track])
    searcher = FakeAudioSearcher({})
    store = FakeAudioStore()
    use_case = AcquireTrackAudio(repo, searcher, store)

    await use_case.execute(_TID, _UID)

    result = await repo.get_by_id(_TID, _UID)
    assert result is not None
    assert result.acquisition_status is AcquisitionStatus.FAILED
    assert result.failure_reason is not None
    assert "no_match_found" in result.failure_reason


@pytest.mark.unit
async def test_acquire_skips_already_ready() -> None:
    track = _ready_track()
    repo = InMemoryTrackRepository([track])
    searcher = FakeAudioSearcher({})
    store = FakeAudioStore()
    use_case = AcquireTrackAudio(repo, searcher, store)

    await use_case.execute(_TID, _UID)

    result = await repo.get_by_id(_TID, _UID)
    assert result is not None
    assert result.acquisition_status is AcquisitionStatus.READY
    assert len(store.stored) == 0


@pytest.mark.unit
async def test_acquire_skips_nonexistent_track() -> None:
    repo = InMemoryTrackRepository()
    searcher = FakeAudioSearcher({})
    store = FakeAudioStore()
    use_case = AcquireTrackAudio(repo, searcher, store)

    await use_case.execute(_TID, _UID)

    assert len(store.stored) == 0


@pytest.mark.unit
async def test_acquire_uses_isrc_tier_when_available() -> None:
    track = _pending_track(isrc="USAT12345678")
    repo = InMemoryTrackRepository([track])
    searcher = FakeAudioSearcher({
        "ytmsearch:USAT12345678": [_GOOD_CANDIDATE],
    })
    store = FakeAudioStore()
    use_case = AcquireTrackAudio(repo, searcher, store)

    await use_case.execute(_TID, _UID)

    assert searcher.queries[0] == "ytmsearch:USAT12345678"
    result = await repo.get_by_id(_TID, _UID)
    assert result is not None
    assert result.acquisition_status is AcquisitionStatus.READY
