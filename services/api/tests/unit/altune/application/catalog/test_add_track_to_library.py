"""Unit tests for the AddTrackToLibrary use case.

Builds a Track from a save request, persists via the repository (which dedups
on the natural key), and emits TrackAddedToLibrary on a fresh save only.
Covers spec AC#5, AC#7, and the server-side guard behind AC#9.
"""

from __future__ import annotations

from typing import Any
from uuid import UUID

import pytest
from altune.application.catalog.add_track_to_library import (
    AddTrackToLibrary,
    AddTrackToLibraryInput,
)
from structlog.testing import capture_logs
from tests._doubles.in_memory_track_repository import InMemoryTrackRepository

from altune.domain.catalog.acquisition_status import AcquisitionStatus
from altune.domain.shared.user_id import UserId

_USER = UserId(UUID("00000000-0000-0000-0000-0000000000aa"))


def _input(**over: Any) -> AddTrackToLibraryInput:
    base: dict[str, Any] = {
        "user_id": _USER,
        "title": "Song",
        "artist": "Artist",
        "album": "Album",
        "duration_seconds": 180,
        "artwork_url": "https://img.example/x.jpg",
    }
    base.update(over)
    return AddTrackToLibraryInput(**base)


@pytest.mark.unit
async def test_add_track_creates_pending_track_with_metadata() -> None:
    uc = AddTrackToLibrary(InMemoryTrackRepository())

    out = await uc.execute(_input())

    assert out.created is True
    assert out.track.title == "Song"
    assert out.track.artwork_url == "https://img.example/x.jpg"
    assert out.track.acquisition_status is AcquisitionStatus.PENDING


@pytest.mark.unit
async def test_add_track_dedupes_second_identical_save() -> None:
    uc = AddTrackToLibrary(InMemoryTrackRepository())

    first = await uc.execute(_input())
    second = await uc.execute(_input())

    assert second.created is False
    assert second.track.id == first.track.id


@pytest.mark.unit
async def test_add_track_emits_event_on_create_and_skips_on_duplicate() -> None:
    uc = AddTrackToLibrary(InMemoryTrackRepository())

    with capture_logs() as logs:
        await uc.execute(_input())
        await uc.execute(_input())  # dedup hit — no event

    emitted = [entry for entry in logs if entry.get("event") == "track_added_to_library"]
    assert len(emitted) == 1


@pytest.mark.unit
async def test_add_track_rejects_empty_artist() -> None:
    uc = AddTrackToLibrary(InMemoryTrackRepository())

    with pytest.raises(ValueError, match=r"artist"):
        await uc.execute(_input(artist=""))
