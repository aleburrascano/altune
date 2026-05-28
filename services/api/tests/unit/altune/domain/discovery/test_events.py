"""Discovery domain events — slice 9.

Past-tense + immutable; carry occurred_at. Per ADR-0007.
"""

from __future__ import annotations

import dataclasses
from datetime import UTC, datetime
from uuid import UUID

import pytest
from altune.domain.discovery.events import ResultClicked, SearchPerformed

from altune.domain.discovery.confidence import Confidence
from altune.domain.shared.user_id import UserId

_USER = UserId(UUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))
_NOW = datetime(2026, 5, 27, 12, 0, tzinfo=UTC)


@pytest.mark.unit
def test_search_performed_is_immutable_with_occurred_at() -> None:
    ev = SearchPerformed(
        query_norm="beatles",
        user_id=_USER,
        occurred_at=_NOW,
        total_results=42,
        partial=False,
    )
    assert ev.occurred_at == _NOW
    with pytest.raises(dataclasses.FrozenInstanceError):
        ev.total_results = 0  # type: ignore[misc]


@pytest.mark.unit
def test_result_clicked_is_immutable() -> None:
    ev = ResultClicked(
        user_id=_USER,
        query_norm="beatles",
        result_signature="track:let-it-be:beatles",
        position=0,
        confidence=Confidence.HIGH,
        occurred_at=_NOW,
    )
    assert ev.position == 0
    with pytest.raises(dataclasses.FrozenInstanceError):
        ev.position = 5  # type: ignore[misc]
