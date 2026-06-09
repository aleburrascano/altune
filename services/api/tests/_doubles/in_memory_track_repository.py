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

from altune.domain.catalog.dedup import dedup_key

if TYPE_CHECKING:
    from collections.abc import Iterable, Sequence

    from altune.domain.catalog.track import Track
    from altune.domain.catalog.track_id import TrackId
    from altune.domain.shared.user_id import UserId


class InMemoryTrackRepository:
    def __init__(self, tracks: Iterable[Track] = ()) -> None:
        self._tracks: list[Track] = list(tracks)

    def _key(self, track: Track) -> tuple[object, str]:
        return (track.user_id, dedup_key(track.title, track.artist, track.album))

    async def get_by_id(self, track_id: TrackId, user_id: UserId) -> Track | None:
        for t in self._tracks:
            if t.id == track_id and t.user_id == user_id:
                return t
        return None

    async def delete(self, track_id: TrackId, user_id: UserId) -> bool:
        for i, t in enumerate(self._tracks):
            if t.id == track_id and t.user_id == user_id:
                self._tracks.pop(i)
                return True
        return False

    async def update(self, track: Track) -> Track:
        for i, existing in enumerate(self._tracks):
            if existing.id == track.id:
                self._tracks[i] = track
                return track
        msg = f"Track {track.id} not found"
        raise ValueError(msg)

    async def add(self, track: Track) -> tuple[Track, bool]:
        key = self._key(track)
        for existing in self._tracks:
            if self._key(existing) == key:
                return existing, False
        self._tracks.append(track)
        return track, True

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
