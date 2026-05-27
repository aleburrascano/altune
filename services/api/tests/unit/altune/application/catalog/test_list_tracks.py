"""ListTracks use case behavior — pagination, has_more, output shape.

Slice 3 of view-library. RED: stub returns empty regardless of seed; GREEN
implements the real has_more derivation + repository delegation.
"""

from __future__ import annotations

from datetime import UTC, datetime, timedelta
from uuid import UUID

import pytest
from tests._doubles.in_memory_track_repository import InMemoryTrackRepository

from altune.application.catalog.list_tracks import (
    ListTracks,
    ListTracksInput,
)
from altune.domain.catalog.track import Track
from altune.domain.catalog.track_id import TrackId
from altune.domain.shared.user_id import UserId

_USER = UserId(UUID("00000000-0000-0000-0000-000000000001"))
_BASE = datetime(2026, 5, 1, 12, 0, tzinfo=UTC)


def _track(i: int) -> Track:
    return Track(
        id=TrackId(UUID(f"00000000-0000-0000-0000-{i:012x}")),
        user_id=_USER,
        title=f"T{i}",
        artist="A",
        album=None,
        duration_seconds=None,
        added_at=_BASE + timedelta(minutes=i),
    )


@pytest.mark.unit
async def test_list_tracks_has_more_true_when_more_rows_exist() -> None:
    repo = InMemoryTrackRepository([_track(i) for i in range(10)])
    use_case = ListTracks(repo)

    output = await use_case.execute(ListTracksInput(user_id=_USER, limit=3, offset=0))

    assert output.has_more is True
    assert output.total == 10
    assert len(output.items) == 3


@pytest.mark.unit
async def test_list_tracks_has_more_false_on_exact_last_page() -> None:
    repo = InMemoryTrackRepository([_track(i) for i in range(6)])
    use_case = ListTracks(repo)

    output = await use_case.execute(ListTracksInput(user_id=_USER, limit=3, offset=3))

    assert output.has_more is False
    assert output.total == 6
    assert len(output.items) == 3


@pytest.mark.unit
async def test_list_tracks_has_more_false_when_empty() -> None:
    repo = InMemoryTrackRepository([])
    use_case = ListTracks(repo)

    output = await use_case.execute(ListTracksInput(user_id=_USER, limit=50, offset=0))

    assert output.has_more is False
    assert output.total == 0
    assert output.items == ()


@pytest.mark.unit
async def test_list_tracks_returns_items_from_repository() -> None:
    tracks = [_track(i) for i in range(3)]
    repo = InMemoryTrackRepository(tracks)
    use_case = ListTracks(repo)

    output = await use_case.execute(ListTracksInput(user_id=_USER, limit=50, offset=0))

    # InMemory sorts by (added_at DESC, id DESC); the largest i has the latest added_at.
    assert [t.id for t in output.items] == [tracks[2].id, tracks[1].id, tracks[0].id]


@pytest.mark.unit
async def test_list_tracks_preserves_input_pagination_in_output() -> None:
    repo = InMemoryTrackRepository([_track(i) for i in range(10)])
    use_case = ListTracks(repo)

    output = await use_case.execute(ListTracksInput(user_id=_USER, limit=4, offset=2))

    assert output.limit == 4
    assert output.offset == 2
