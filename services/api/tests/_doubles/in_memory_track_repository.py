"""InMemoryTrackRepository — test fake implementing the TrackRepository port.

Default choice for unit-testing use cases per .claude/rules/tests.md:
"Fake — working implementation simpler than production". Matches the SQL
contract: ordering by ``(added_at DESC, id DESC)``, user-scoped, paginated
by ``limit`` / ``offset``.

The shared-port-contract test (Slice 5b) runs the same scenarios against
this fake AND the SqlAlchemyTrackRepository to catch any drift between
the two implementations.
"""

from __future__ import annotations

from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from collections.abc import Iterable, Sequence

    from altune.domain.catalog.track import Track
    from altune.domain.shared.user_id import UserId


class InMemoryTrackRepository:
    def __init__(self, tracks: Iterable[Track] = ()) -> None:
        self._tracks: list[Track] = list(tracks)

    async def list_for_user(
        self,
        user_id: UserId,
        limit: int,
        offset: int,
    ) -> tuple[Sequence[Track], int]:
        # Filter by user, then sort (added_at DESC, id DESC) by sorting the
        # tuple (added_at, id_value) ascending and reversing — id.value is a
        # UUID, which is lexicographically comparable.
        user_tracks = [t for t in self._tracks if t.user_id == user_id]
        user_tracks.sort(key=lambda t: (t.added_at, t.id.value), reverse=True)
        total = len(user_tracks)
        page = tuple(user_tracks[offset : offset + limit])
        return page, total
