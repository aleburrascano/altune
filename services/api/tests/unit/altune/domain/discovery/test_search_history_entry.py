"""SearchHistoryEntry + SearchClick aggregates — slice 8 of discover-music-v1."""

from __future__ import annotations

from datetime import UTC, datetime
from uuid import UUID

import pytest
from altune.domain.discovery.search_click import SearchClick, SearchClickId
from altune.domain.discovery.search_history_entry import (
    SearchHistoryEntry,
    SearchHistoryEntryId,
)

from altune.domain.discovery.confidence import Confidence
from altune.domain.shared.user_id import UserId

_USER = UserId(UUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))
_NOW = datetime(2026, 5, 27, 12, 0, tzinfo=UTC)


@pytest.mark.unit
def test_search_history_entry_equals_by_id() -> None:
    eid = SearchHistoryEntryId(UUID("11111111-1111-1111-1111-111111111111"))
    a = SearchHistoryEntry(
        id=eid,
        user_id=_USER,
        query="the beatles",
        query_norm="beatles",
        executed_at=_NOW,
        result_clicked_signature=None,
    )
    b = SearchHistoryEntry(
        id=eid,
        user_id=_USER,
        query="different raw",  # other attrs differ
        query_norm="other",
        executed_at=_NOW,
        result_clicked_signature="sig",
    )
    assert a == b
    assert hash(a) == hash(b)


@pytest.mark.unit
def test_search_history_entry_rejects_empty_query() -> None:
    with pytest.raises(ValueError, match="query"):
        SearchHistoryEntry(
            id=SearchHistoryEntryId(UUID("22222222-2222-2222-2222-222222222222")),
            user_id=_USER,
            query="",
            query_norm="x",
            executed_at=_NOW,
            result_clicked_signature=None,
        )


@pytest.mark.unit
def test_search_click_position_must_be_non_negative() -> None:
    with pytest.raises(ValueError, match="position"):
        SearchClick(
            id=SearchClickId(UUID("33333333-3333-3333-3333-333333333333")),
            user_id=_USER,
            query_norm="beatles",
            result_signature="track:let-it-be:beatles",
            position=-1,
            confidence=Confidence.HIGH,
            clicked_at=_NOW,
        )


@pytest.mark.unit
def test_search_click_equals_by_id() -> None:
    cid = SearchClickId(UUID("44444444-4444-4444-4444-444444444444"))
    a = SearchClick(
        id=cid,
        user_id=_USER,
        query_norm="beatles",
        result_signature="sig",
        position=0,
        confidence=Confidence.HIGH,
        clicked_at=_NOW,
    )
    b = SearchClick(
        id=cid,
        user_id=_USER,
        query_norm="other",
        result_signature="other-sig",
        position=5,
        confidence=Confidence.LOW,
        clicked_at=_NOW,
    )
    assert a == b
