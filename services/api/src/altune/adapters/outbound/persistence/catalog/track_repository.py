"""SqlAlchemyTrackRepository — outbound persistence adapter for Track.

Implements `altune.application.catalog.ports.TrackRepository` against a real
Postgres via an `AsyncSession`. The session is supplied by the FastAPI
dependency in `adapters/inbound/http/catalog/` and disposed via the lifespan
in `platform/app.py`.

STUB: GREEN commit implements the SQL. Currently returns an empty page so
the integration tests fail meaningfully.
"""

from __future__ import annotations

from typing import TYPE_CHECKING

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
        # STUB
        return (), 0
