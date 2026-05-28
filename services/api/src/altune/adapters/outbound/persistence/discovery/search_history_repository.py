"""SqlAlchemySearchHistoryRepository — slice 37.

Implements `altune.application.discovery.ports.SearchHistoryRepository`.
- insert: persist one SearchHistoryEntry row.
- trim_to_n: keep the latest n rows for the user.
- list_distinct_recent: latest distinct query_norm rows newest-first (AC#13).
"""

from __future__ import annotations

from typing import TYPE_CHECKING

from sqlalchemy import delete, func, select

from altune.adapters.outbound.persistence.discovery.search_history_row import (
    SearchHistoryRow,
)

if TYPE_CHECKING:
    from sqlalchemy.ext.asyncio import AsyncSession

    from altune.domain.discovery.search_history_entry import SearchHistoryEntry
    from altune.domain.shared.user_id import UserId


class SqlAlchemySearchHistoryRepository:
    def __init__(self, session: AsyncSession) -> None:
        self._session = session

    async def insert(self, entry: SearchHistoryEntry) -> None:
        row = SearchHistoryRow.from_domain(entry)
        self._session.add(row)
        await self._session.flush()

    async def trim_to_n(self, user_id: UserId, n: int) -> None:
        # Find the executed_at threshold below which we drop rows.
        # Select the nth-newest row's executed_at; delete anything older.
        keep_subq = (
            select(SearchHistoryRow.id)
            .where(SearchHistoryRow.user_id == user_id.value)
            .order_by(
                SearchHistoryRow.executed_at.desc(),
                SearchHistoryRow.id.desc(),
            )
            .limit(n)
            .subquery()
        )
        stmt = (
            delete(SearchHistoryRow)
            .where(SearchHistoryRow.user_id == user_id.value)
            .where(SearchHistoryRow.id.notin_(select(keep_subq.c.id)))
        )
        await self._session.execute(stmt)
        await self._session.flush()

    async def list_distinct_recent(
        self,
        user_id: UserId,
        limit: int,
    ) -> tuple[SearchHistoryEntry, ...]:
        # Group by query_norm; pick the most recent row per group; order
        # groups by MAX(executed_at) DESC; take top `limit`. Two-step:
        # (1) find the latest id per query_norm, (2) fetch full rows.
        latest_per_norm = (
            select(
                SearchHistoryRow.query_norm,
                func.max(SearchHistoryRow.executed_at).label("max_executed_at"),
            )
            .where(SearchHistoryRow.user_id == user_id.value)
            .group_by(SearchHistoryRow.query_norm)
            .order_by(func.max(SearchHistoryRow.executed_at).desc())
            .limit(limit)
            .subquery()
        )
        # Join back to get the row with max executed_at for each query_norm.
        stmt = (
            select(SearchHistoryRow)
            .join(
                latest_per_norm,
                (SearchHistoryRow.query_norm == latest_per_norm.c.query_norm)
                & (SearchHistoryRow.executed_at == latest_per_norm.c.max_executed_at)
                & (SearchHistoryRow.user_id == user_id.value),
            )
            .order_by(SearchHistoryRow.executed_at.desc(), SearchHistoryRow.id.desc())
        )
        result = await self._session.execute(stmt)
        rows = result.scalars().all()
        return tuple(row.to_domain() for row in rows)
