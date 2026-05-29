"""InMemorySearchClickRepository — Fowler-style fake with sliding-window dedup."""

from __future__ import annotations

from datetime import timedelta
from typing import TYPE_CHECKING

from altune.application.discovery.ports import ClickInsertOutcome

if TYPE_CHECKING:
    from altune.domain.discovery.search_click import SearchClick


class InMemorySearchClickRepository:
    """Sliding-window dedup mirroring the SqlAlchemy repo behavior."""

    def __init__(self) -> None:
        self._rows: list[SearchClick] = []

    async def insert_if_outside_window(
        self, click: SearchClick, window_seconds: int
    ) -> ClickInsertOutcome:
        window = timedelta(seconds=window_seconds)
        # Find the most recent identical (user_id, query_norm, result_signature)
        # row by clicked_at; dedup if it's within the window.
        candidates = [
            r
            for r in self._rows
            if r.user_id == click.user_id
            and r.query_norm == click.query_norm
            and r.result_signature == click.result_signature
        ]
        if candidates:
            most_recent = max(candidates, key=lambda r: r.clicked_at)
            if click.clicked_at - most_recent.clicked_at < window:
                return ClickInsertOutcome(inserted=False, deduped_against_id=most_recent.id.value)
        self._rows.append(click)
        return ClickInsertOutcome(inserted=True, deduped_against_id=None)
