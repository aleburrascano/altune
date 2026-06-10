"""TagStep tests — ID3 tagging on a real temp MP3."""

from __future__ import annotations

from datetime import UTC, datetime
from pathlib import Path
from uuid import UUID

import pytest

from altune.application.catalog.acquisition.context import AcquisitionContext
from altune.application.catalog.acquisition.steps.tag import TagStep
from altune.domain.catalog.track import Track
from altune.domain.catalog.track_id import TrackId
from altune.domain.shared.user_id import UserId

_TID = TrackId(UUID("11111111-1111-1111-1111-111111111111"))
_UID = UserId(UUID("00000000-0000-0000-0000-000000000001"))


def _track() -> Track:
    return Track(
        id=_TID,
        user_id=_UID,
        title="Test Song",
        artist="Test Artist",
        album="Test Album",
        duration_seconds=180,
        added_at=datetime(2026, 1, 1, tzinfo=UTC),
        year=2024,
        genre="Pop",
        track_number=3,
        album_artist="VA",
    )


def _create_taggable_mp3(path: Path) -> None:
    """Create an empty MP3 that mutagen can open and tag via ID3."""
    from mutagen.id3 import ID3

    path.write_bytes(b"")
    tags = ID3()
    tags.save(path)


@pytest.mark.unit
async def test_tag_step_writes_title_and_artist(tmp_path: Path) -> None:
    mp3_path = tmp_path / "test.mp3"
    _create_taggable_mp3(mp3_path)
    ctx = AcquisitionContext(track=_track(), temp_path=mp3_path)
    step = TagStep()

    await step.execute(ctx)

    from mutagen.id3 import ID3

    tags = ID3(mp3_path)
    assert str(tags["TIT2"]) == "Test Song"
    assert str(tags["TPE1"]) == "Test Artist"
    assert str(tags["TALB"]) == "Test Album"
    assert str(tags["TDRC"]) == "2024"
    assert str(tags["TRCK"]) == "3"
    assert str(tags["TPE2"]) == "VA"
    assert str(tags["TCON"]) == "Pop"


@pytest.mark.unit
async def test_tag_step_survives_invalid_file(tmp_path: Path) -> None:
    bad_path = tmp_path / "not_an_mp3.mp3"
    bad_path.write_bytes(b"this is not an mp3")
    ctx = AcquisitionContext(track=_track(), temp_path=bad_path)
    step = TagStep()

    result = await step.execute(ctx)

    assert result is ctx
