"""RecordClick — slice 41 tests."""

from __future__ import annotations

from uuid import UUID

import pytest
from tests._doubles.in_memory_search_click_repository import (
    InMemorySearchClickRepository,
)

from altune.application.discovery.record_click import (
    RecordClick,
    RecordClickInput,
    compute_result_signature,
)
from altune.domain.discovery.confidence import Confidence
from altune.domain.discovery.result_kind import ResultKind
from altune.domain.shared.user_id import UserId

_USER = UserId(UUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))


@pytest.mark.unit
def test_compute_result_signature_is_deterministic() -> None:
    a = compute_result_signature(ResultKind.TRACK, "Let It Be", "The Beatles")
    b = compute_result_signature(ResultKind.TRACK, "Let It Be", "The Beatles")
    assert a == b
    assert len(a) == 12


@pytest.mark.unit
def test_compute_result_signature_collapses_diacritic_variants() -> None:
    a = compute_result_signature(ResultKind.TRACK, "Beyoncé", "Artist")
    b = compute_result_signature(ResultKind.TRACK, "Beyonce", "Artist")
    assert a == b


@pytest.mark.unit
def test_compute_result_signature_differs_by_kind() -> None:
    track = compute_result_signature(ResultKind.TRACK, "Same", "Same")
    artist = compute_result_signature(ResultKind.ARTIST, "Same", "Same")
    assert track != artist


@pytest.mark.unit
@pytest.mark.asyncio
async def test_record_click_inserts_outside_window() -> None:
    repo = InMemorySearchClickRepository()
    use_case = RecordClick(click_repo=repo)
    output = await use_case.execute(
        RecordClickInput(
            user_id=_USER,
            query_norm="beatles",
            kind=ResultKind.TRACK,
            title="Let It Be",
            subtitle="The Beatles",
            position=1,
            confidence=Confidence.HIGH,
        )
    )
    assert output.outcome.inserted is True
    assert output.outcome.deduped_against_id is None


@pytest.mark.unit
@pytest.mark.asyncio
async def test_record_click_dedupes_identical_within_window() -> None:
    repo = InMemorySearchClickRepository()
    use_case = RecordClick(click_repo=repo)
    request = RecordClickInput(
        user_id=_USER,
        query_norm="beatles",
        kind=ResultKind.TRACK,
        title="Let It Be",
        subtitle="The Beatles",
        position=1,
        confidence=Confidence.HIGH,
    )
    first = await use_case.execute(request)
    second = await use_case.execute(request)
    assert first.outcome.inserted is True
    assert second.outcome.inserted is False
    assert second.outcome.deduped_against_id is not None
