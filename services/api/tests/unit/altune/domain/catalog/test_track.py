"""Track aggregate invariants + identity-based equality.

Slice 1 of view-library. RED commit: this file fails on the invariant tests
because Track.__post_init__ doesn't enforce them yet, and on the id-equality
test because default dataclass equality is attribute-based.
"""

from __future__ import annotations

from dataclasses import FrozenInstanceError
from datetime import UTC, datetime
from uuid import UUID

import pytest

from altune.domain.catalog.acquisition_status import AcquisitionStatus
from altune.domain.catalog.track import Track
from altune.domain.catalog.track_id import TrackId
from altune.domain.shared.user_id import UserId

_TRACK_ID_A = TrackId(UUID("11111111-1111-1111-1111-111111111111"))
_TRACK_ID_B = TrackId(UUID("22222222-2222-2222-2222-222222222222"))
_USER_ID = UserId(UUID("00000000-0000-0000-0000-000000000001"))
_ADDED = datetime(2026, 5, 1, 12, 0, tzinfo=UTC)


def _valid_track(
    *,
    id: TrackId = _TRACK_ID_A,
    title: str = "Song of the Sirens",
    artist: str = "The Bards",
    album: str | None = "Wanderlust",
    duration_seconds: int | None = 180,
    year: int | None = None,
    track_number: int | None = None,
) -> Track:
    return Track(
        id=id,
        user_id=_USER_ID,
        title=title,
        artist=artist,
        album=album,
        duration_seconds=duration_seconds,
        added_at=_ADDED,
        year=year,
        track_number=track_number,
    )


@pytest.mark.unit
def test_track_rejects_empty_title() -> None:
    with pytest.raises(ValueError, match=r"title"):
        _valid_track(title="")


@pytest.mark.unit
def test_track_rejects_empty_artist() -> None:
    with pytest.raises(ValueError, match=r"artist"):
        _valid_track(artist="")


@pytest.mark.unit
def test_track_rejects_negative_duration_seconds() -> None:
    with pytest.raises(ValueError, match=r"duration"):
        _valid_track(duration_seconds=-1)


@pytest.mark.unit
def test_track_accepts_null_album_and_duration() -> None:
    track = _valid_track(album=None, duration_seconds=None)
    assert track.album is None
    assert track.duration_seconds is None


@pytest.mark.unit
def test_track_is_frozen() -> None:
    track = _valid_track()
    with pytest.raises(FrozenInstanceError):
        track.title = "renamed"  # type: ignore[misc]  # intentional: testing immutability


@pytest.mark.unit
def test_track_equality_by_id_same_id_different_attrs() -> None:
    track_a = _valid_track(title="Song A")
    track_b = _valid_track(title="Song B")
    assert track_a == track_b


@pytest.mark.unit
def test_track_inequality_by_id_different_id_same_attrs() -> None:
    track_a = _valid_track(id=_TRACK_ID_A)
    track_b = _valid_track(id=_TRACK_ID_B)
    assert track_a != track_b


@pytest.mark.unit
def test_track_hash_matches_equality() -> None:
    track_a = _valid_track(title="Song A")
    track_b = _valid_track(title="Song B")  # same id as track_a
    assert hash(track_a) == hash(track_b)
    assert {track_a, track_b} == {track_a}


@pytest.mark.unit
def test_track_defaults_acquisition_status_to_pending() -> None:
    track = _valid_track()
    assert track.acquisition_status is AcquisitionStatus.PENDING


@pytest.mark.unit
def test_track_artwork_url_defaults_to_none() -> None:
    track = _valid_track()
    assert track.artwork_url is None


@pytest.mark.unit
def test_track_accepts_explicit_artwork_url_and_status() -> None:
    track = Track(
        id=_TRACK_ID_A,
        user_id=_USER_ID,
        title="Song",
        artist="Artist",
        album=None,
        duration_seconds=None,
        added_at=_ADDED,
        artwork_url="https://img.example/cover.jpg",
        acquisition_status=AcquisitionStatus.PENDING,
    )
    assert track.artwork_url == "https://img.example/cover.jpg"
    assert track.acquisition_status is AcquisitionStatus.PENDING


@pytest.mark.unit
def test_track_accepts_metadata_fields() -> None:
    track = _valid_track()
    track_with_meta = Track(
        id=_TRACK_ID_A,
        user_id=_USER_ID,
        title="Song",
        artist="Artist",
        album="Album",
        duration_seconds=180,
        added_at=_ADDED,
        year=2024,
        genre="Rap/Hip Hop",
        track_number=5,
        album_artist="Various Artists",
        isrc="USRC12345678",
    )
    assert track_with_meta.year == 2024
    assert track_with_meta.genre == "Rap/Hip Hop"
    assert track_with_meta.track_number == 5
    assert track_with_meta.album_artist == "Various Artists"
    assert track_with_meta.isrc == "USRC12345678"
    assert track.year is None
    assert track.genre is None
    assert track.track_number is None
    assert track.album_artist is None
    assert track.isrc is None


@pytest.mark.unit
def test_track_rejects_non_positive_year() -> None:
    with pytest.raises(ValueError, match=r"year"):
        _valid_track(year=0)


@pytest.mark.unit
def test_track_rejects_non_positive_track_number() -> None:
    with pytest.raises(ValueError, match=r"track_number"):
        _valid_track(track_number=0)


@pytest.mark.unit
def test_track_audio_ref_requires_ready_status() -> None:
    with pytest.raises(ValueError, match=r"audio_ref requires"):
        Track(
            id=_TRACK_ID_A,
            user_id=_USER_ID,
            title="Song",
            artist="Artist",
            album=None,
            duration_seconds=None,
            added_at=_ADDED,
            audio_ref="some/path.mp3",
            acquisition_status=AcquisitionStatus.PENDING,
        )


@pytest.mark.unit
def test_track_ready_status_requires_audio_ref() -> None:
    with pytest.raises(ValueError, match=r"READY requires audio_ref"):
        Track(
            id=_TRACK_ID_A,
            user_id=_USER_ID,
            title="Song",
            artist="Artist",
            album=None,
            duration_seconds=None,
            added_at=_ADDED,
            acquisition_status=AcquisitionStatus.READY,
        )


@pytest.mark.unit
def test_track_accepts_audio_ref_with_ready_status() -> None:
    track = Track(
        id=_TRACK_ID_A,
        user_id=_USER_ID,
        title="Song",
        artist="Artist",
        album=None,
        duration_seconds=None,
        added_at=_ADDED,
        audio_ref="user-id/Artist/Album/Song.mp3",
        acquisition_status=AcquisitionStatus.READY,
    )
    assert track.audio_ref == "user-id/Artist/Album/Song.mp3"
    assert track.acquisition_status is AcquisitionStatus.READY
