"""Catalog ports — interfaces owned by the application layer.

Implementations live in `adapters/outbound/persistence/catalog/`. Tests use
in-memory fakes from `tests/_doubles/`. The use cases in this package call
these interfaces, never the concrete adapters.
"""

from __future__ import annotations

from typing import TYPE_CHECKING, Protocol

if TYPE_CHECKING:
    from collections.abc import Sequence

    from altune.domain.catalog.track import Track
    from altune.domain.shared.user_id import UserId


class TrackRepository(Protocol):
    """A read+write port over the Track aggregate.

    For view-library (read-only), only `list_for_user` is required; write
    methods land with the future write-path specs (add-track-manually,
    edit-track-metadata, delete-track) so the port grows by feature, not
    pre-emptively.
    """

    async def list_for_user(
        self,
        user_id: UserId,
        limit: int,
        offset: int,
    ) -> tuple[Sequence[Track], int]:
        """Return one page of the user's tracks plus the total count.

        Ordering is ``(added_at DESC, id DESC)`` — id is the stable tiebreaker
        per the spec's AC#1. Implementations enforce user-scoping at the
        storage boundary (e.g., ``WHERE user_id = $1`` in SQL).

        ``total`` is a snapshot at the moment of this call; concurrent writes
        between paged requests can shift values. Acceptable for v1 per the
        spec's Design Considerations.
        """
        ...
