"""InMemoryTrackRepository — test fake implementing the TrackRepository port.

Default choice for unit-testing use cases per .claude/rules/tests.md:
"Fake — working implementation simpler than production". Matches the SQL
contract: ordering by ``(added_at DESC, id DESC)``, user-scoped.

STUB: GREEN commit fills in filtering, sorting, and pagination. Currently
returns an empty page so the RED tests fail.
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
        # STUB
        return (), 0
