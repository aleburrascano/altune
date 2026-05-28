"""SqlAlchemySearchClickRepository — slice 40.

Implements `altune.application.discovery.ports.SearchClickRepository`.
Sliding-window dedup: a new click is persisted only if no identical
(user_id, query_norm, result_signature) row was clicked within the
last `window_seconds` (AC#16).
"""

from __future__ import annotations

from datetime import UTC, datetime, timedelta
from typing import TYPE_CHECKING

from sqlalchemy import select

from altune.adapters.outbound.persistence.discovery.search_click_row import (
    SearchClickRow,
)
from altune.application.discovery.ports import ClickInsertOutcome

if TYPE_CHECKING:
    from sqlalchemy.ext.asyncio import AsyncSession

    from altune.domain.discovery.search_click import SearchClick


class SqlAlchemySearchClickRepository:
    def __init__(self, session: AsyncSession) -> None:
        self._session = session

    async def insert_if_outside_window(
        self,
        click: SearchClick,
        window_seconds: int,
    ) -> ClickInsertOutcome:
        threshold = datetime.now(UTC) - timedelta(seconds=window_seconds)
        existing_stmt = (
            select(SearchClickRow.id)
            .where(SearchClickRow.user_id == click.user_id.value)
            .where(SearchClickRow.query_norm == click.query_norm)
            .where(SearchClickRow.result_signature == click.result_signature)
            .where(SearchClickRow.clicked_at > threshold)
            .order_by(SearchClickRow.clicked_at.desc())
            .limit(1)
        )
        result = await self._session.execute(existing_stmt)
        deduped_id = result.scalar_one_or_none()
        if deduped_id is not None:
            return ClickInsertOutcome(inserted=False, deduped_against_id=deduped_id)
        row = SearchClickRow.from_domain(click)
        self._session.add(row)
        await self._session.flush()
        return ClickInsertOutcome(inserted=True, deduped_against_id=None)
