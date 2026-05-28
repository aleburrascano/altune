"""InMemorySearchHistoryRepository — Fowler-style fake."""

from __future__ import annotations

from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from altune.domain.discovery.search_history_entry import SearchHistoryEntry
    from altune.domain.shared.user_id import UserId


class InMemorySearchHistoryRepository:
    """In-process search-history backend; insertion-ordered list per user."""

    def __init__(self) -> None:
        self._rows: dict[UserId, list[SearchHistoryEntry]] = {}

    async def insert(self, entry: SearchHistoryEntry) -> None:
        self._rows.setdefault(entry.user_id, []).append(entry)

    async def trim_to_n(self, user_id: UserId, n: int) -> None:
        rows = self._rows.get(user_id, [])
        if len(rows) <= n:
            return
        sorted_rows = sorted(rows, key=lambda r: r.executed_at, reverse=True)
        self._rows[user_id] = sorted_rows[:n]

    async def list_distinct_recent(
        self, user_id: UserId, limit: int
    ) -> tuple[SearchHistoryEntry, ...]:
        rows = sorted(
            self._rows.get(user_id, []), key=lambda r: r.executed_at, reverse=True
        )
        seen: set[str] = set()
        out: list[SearchHistoryEntry] = []
        for r in rows:
            if r.query_norm in seen:
                continue
            seen.add(r.query_norm)
            out.append(r)
            if len(out) >= limit:
                break
        return tuple(out)
