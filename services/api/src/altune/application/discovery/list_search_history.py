"""ListSearchHistory — slice 39 use case for GET /v1/discovery/search-history."""

from __future__ import annotations

from dataclasses import dataclass
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from altune.application.discovery.ports import SearchHistoryRepository
    from altune.domain.discovery.search_history_entry import SearchHistoryEntry
    from altune.domain.shared.user_id import UserId


@dataclass(frozen=True, slots=True)
class ListSearchHistoryInput:
    user_id: UserId
    limit: int = 10


@dataclass(frozen=True, slots=True)
class ListSearchHistoryOutput:
    items: tuple[SearchHistoryEntry, ...]
    total: int


@dataclass
class ListSearchHistory:
    history_repo: SearchHistoryRepository

    async def execute(self, request: ListSearchHistoryInput) -> ListSearchHistoryOutput:
        items = await self.history_repo.list_distinct_recent(
            request.user_id, request.limit
        )
        return ListSearchHistoryOutput(items=items, total=len(items))
