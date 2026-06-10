"""StoreStep tests."""

from __future__ import annotations

from datetime import UTC, datetime
from pathlib import Path
from uuid import UUID

import pytest

from altune.application.catalog.acquisition.context import AcquisitionContext
from altune.application.catalog.acquisition.steps.store import StoreStep, _sanitize
from altune.domain.catalog.track import Track
from altune.domain.catalog.track_id import TrackId
from altune.domain.shared.user_id import UserId

_TID = TrackId(UUID("11111111-1111-1111-1111-111111111111"))
_UID = UserId(UUID("00000000-0000-0000-0000-000000000001"))


def _track() -> Track:
    return Track(
        id=_TID,
        user_id=_UID,
        title="Song Title",
        artist="Artist Name",
        album="Album Name",
        duration_seconds=200,
        added_at=datetime(2026, 1, 1, tzinfo=UTC),
    )


class FakeAudioStore:
    def __init__(self) -> None:
        self.stored: list[tuple[Path, str]] = []

    async def store(self, source_path: Path, audio_ref: str) -> str:
        self.stored.append((source_path, audio_ref))
        return audio_ref


@pytest.mark.unit
async def test_store_step_calls_audio_store_and_sets_ref(tmp_path: Path) -> None:
    fake = FakeAudioStore()
    step = StoreStep(fake)
    temp_file = tmp_path / "audio.mp3"
    temp_file.write_bytes(b"\x00")
    ctx = AcquisitionContext(track=_track(), temp_path=temp_file)

    result = await step.execute(ctx)

    assert result.audio_ref is not None
    assert "Artist Name" in result.audio_ref
    assert "Song Title.mp3" in result.audio_ref
    assert len(fake.stored) == 1


@pytest.mark.unit
def test_sanitize_strips_forbidden_chars() -> None:
    assert _sanitize('Song: "The Remix"') == "Song The Remix"
    assert _sanitize("AC/DC") == "ACDC"
    assert _sanitize("") == "Unknown"
