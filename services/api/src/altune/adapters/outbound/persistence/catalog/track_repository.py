"""SqlAlchemyTrackRepository — outbound persistence adapter for Track.

Implements `altune.application.catalog.ports.TrackRepository` against a real
Postgres via an `AsyncSession`. The session is supplied by the FastAPI
dependency in `adapters/inbound/http/catalog/` and disposed via the lifespan
in `platform/app.py`.
"""

from __future__ import annotations

from typing import TYPE_CHECKING

from sqlalchemy import func, select

from altune.adapters.outbound.persistence.catalog.track_row import TrackRow

if TYPE_CHECKING:
    from collections.abc import Sequence

    from sqlalchemy.ext.asyncio import AsyncSession

    from altune.domain.catalog.track import Track
    from altune.domain.shared.user_id import UserId


class SqlAlchemyTrackRepository:
    def __init__(self, session: AsyncSession) -> None:
        self._session = session

    async def list_for_user(
        self,
        user_id: UserId,
        limit: int,
        offset: int,
    ) -> tuple[Sequence[Track], int]:
        # Page: WHERE user_id = $1 ORDER BY added_at DESC, id DESC LIMIT $2 OFFSET $3.
        # id-as-tiebreaker matches the index tracks_user_added_idx from the slice 4
        # migration and the deterministic order required by AC#1.
        page_stmt = (
            select(TrackRow)
            .where(TrackRow.user_id == user_id.value)
            .order_by(TrackRow.added_at.desc(), TrackRow.id.desc())
            .limit(limit)
            .offset(offset)
        )
        # Total: COUNT(*) of all the user's rows (not just this page). Spec accepts
        # per-request snapshot semantics; concurrent writes can shift `total` between
        # paged calls in production. v1 has no writers, so academic.
        count_stmt = (
            select(func.count()).select_from(TrackRow).where(TrackRow.user_id == user_id.value)
        )

        page_result = await self._session.execute(page_stmt)
        rows = page_result.scalars().all()
        count_result = await self._session.execute(count_stmt)
        total: int = count_result.scalar_one()

        items = tuple(row.to_domain() for row in rows)
        return items, total
