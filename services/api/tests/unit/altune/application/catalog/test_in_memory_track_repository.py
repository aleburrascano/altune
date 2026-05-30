"""InMemoryTrackRepository conforms to the TrackRepository contract.

Slice 2 of view-library. RED: stub repo returns empty/zero; GREEN fills in
the filter + sort + paginate behavior the unit tests demand.
"""

from __future__ import annotations

from datetime import UTC, datetime, timedelta
from uuid import UUID

import pytest
from tests._doubles.in_memory_track_repository import InMemoryTrackRepository

from altune.domain.catalog.track import Track
from altune.domain.catalog.track_id import TrackId
from altune.domain.shared.user_id import UserId

_USER_A = UserId(UUID("00000000-0000-0000-0000-000000000001"))
_USER_B = UserId(UUID("00000000-0000-0000-0000-000000000002"))
_BASE = datetime(2026, 5, 1, 12, 0, tzinfo=UTC)


def _track(
    *,
    user: UserId,
    id_hex: str,
    title: str = "T",
    added: datetime | None = None,
) -> Track:
    return Track(
        id=TrackId(UUID(id_hex)),
        user_id=user,
        title=title,
        artist="A",
        album=None,
        duration_seconds=None,
        added_at=added or _BASE,
    )


@pytest.mark.unit
async def test_in_memory_repo_lists_only_current_user_tracks_in_correct_order() -> None:
    user_a_old = _track(user=_USER_A, id_hex="11111111-1111-1111-1111-111111111111", added=_BASE)
    user_a_new = _track(
        user=_USER_A,
        id_hex="22222222-2222-2222-2222-222222222222",
        added=_BASE + timedelta(hours=1),
    )
    user_b_track = _track(user=_USER_B, id_hex="33333333-3333-3333-3333-333333333333")
    repo = InMemoryTrackRepository([user_a_old, user_a_new, user_b_track])

    items, total = await repo.list_for_user(_USER_A, limit=50, offset=0)

    assert [t.id for t in items] == [user_a_new.id, user_a_old.id]
    assert total == 2


@pytest.mark.unit
async def test_in_memory_repo_orders_with_id_desc_tiebreaker_on_same_added_at() -> None:
    same_time = _BASE
    track_low_id = _track(
        user=_USER_A, id_hex="aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", added=same_time
    )
    track_high_id = _track(
        user=_USER_A, id_hex="ffffffff-ffff-ffff-ffff-ffffffffffff", added=same_time
    )
    repo = InMemoryTrackRepository([track_low_id, track_high_id])

    items, _ = await repo.list_for_user(_USER_A, limit=50, offset=0)

    assert [t.id for t in items] == [track_high_id.id, track_low_id.id]


@pytest.mark.unit
async def test_in_memory_repo_paginates_via_limit_and_offset() -> None:
    tracks = [
        _track(
            user=_USER_A,
            id_hex=f"00000000-0000-0000-0000-{i:012x}",
            added=_BASE + timedelta(minutes=i),
        )
        for i in range(5)
    ]
    repo = InMemoryTrackRepository(tracks)

    page_1, total = await repo.list_for_user(_USER_A, limit=2, offset=0)
    page_2, _ = await repo.list_for_user(_USER_A, limit=2, offset=2)
    page_3, _ = await repo.list_for_user(_USER_A, limit=2, offset=4)

    assert len(page_1) == 2
    assert len(page_2) == 2
    assert len(page_3) == 1
    assert total == 5
    # Pages don't overlap.
    ids = [t.id for t in (*page_1, *page_2, *page_3)]
    assert len(set(ids)) == 5


@pytest.mark.unit
async def test_in_memory_repo_returns_total_across_all_user_pages_not_just_page_size() -> None:
    tracks = [
        _track(
            user=_USER_A,
            id_hex=f"00000000-0000-0000-0000-{i:012x}",
            added=_BASE + timedelta(minutes=i),
        )
        for i in range(7)
    ]
    repo = InMemoryTrackRepository(tracks)

    items, total = await repo.list_for_user(_USER_A, limit=3, offset=0)

    assert len(items) == 3
    assert total == 7


@pytest.mark.unit
async def test_in_memory_repo_returns_empty_when_user_has_no_tracks() -> None:
    repo = InMemoryTrackRepository(
        [_track(user=_USER_B, id_hex="11111111-1111-1111-1111-111111111111")]
    )

    items, total = await repo.list_for_user(_USER_A, limit=50, offset=0)

    assert items == ()
    assert total == 0


@pytest.mark.unit
async def test_in_memory_add_persists_new_track_and_returns_created_true() -> None:
    repo = InMemoryTrackRepository()
    track = _track(user=_USER_A, id_hex="11111111-1111-1111-1111-111111111111", title="New")

    persisted, created = await repo.add(track)

    assert created is True
    assert persisted.id == track.id
    _, total = await repo.list_for_user(_USER_A, limit=10, offset=0)
    assert total == 1


@pytest.mark.unit
async def test_in_memory_add_returns_existing_and_created_false_on_duplicate() -> None:
    first = _track(user=_USER_A, id_hex="11111111-1111-1111-1111-111111111111", title="Dup")
    repo = InMemoryTrackRepository([first])
    # Same natural key (case-insensitive title, same artist/album), different id.
    again = _track(user=_USER_A, id_hex="22222222-2222-2222-2222-222222222222", title="dup")

    persisted, created = await repo.add(again)

    assert created is False
    assert persisted.id == first.id  # existing returned, not the new one
    _, total = await repo.list_for_user(_USER_A, limit=10, offset=0)
    assert total == 1


@pytest.mark.unit
async def test_in_memory_add_same_title_different_users_both_created() -> None:
    repo = InMemoryTrackRepository(
        [_track(user=_USER_A, id_hex="11111111-1111-1111-1111-111111111111", title="Same")]
    )
    other_user = _track(user=_USER_B, id_hex="22222222-2222-2222-2222-222222222222", title="Same")

    _, created = await repo.add(other_user)

    assert created is True
