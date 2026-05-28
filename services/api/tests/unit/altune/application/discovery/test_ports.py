"""SearchProvider + QueryCache + repository ports — slices 12+13.

Smoke tests for Protocol shape + Fowler-style test doubles.
"""

from __future__ import annotations

from datetime import UTC, datetime, timedelta
from uuid import UUID, uuid4

import pytest
from tests._doubles.in_memory_query_cache import InMemoryQueryCache
from tests._doubles.in_memory_search_click_repository import InMemorySearchClickRepository
from tests._doubles.in_memory_search_history_repository import (
    InMemorySearchHistoryRepository,
)
from tests._doubles.in_memory_search_provider import InMemorySearchProvider

from altune.application.discovery.ports import (
    ClickInsertOutcome,
    QueryCache,
    SearchClickRepository,
    SearchHistoryRepository,
    SearchProvider,
)
from altune.domain.discovery.confidence import Confidence
from altune.domain.discovery.provider import ProviderName
from altune.domain.discovery.provider_status import ProviderStatus
from altune.domain.discovery.result_kind import ResultKind
from altune.domain.discovery.search_click import SearchClick, SearchClickId
from altune.domain.discovery.search_history_entry import (
    SearchHistoryEntry,
    SearchHistoryEntryId,
)
from altune.domain.discovery.search_result import SearchResult
from altune.domain.discovery.source_ref import SourceRef
from altune.domain.shared.user_id import UserId

_USER = UserId(UUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))
_NOW = datetime(2026, 5, 27, 12, 0, tzinfo=UTC)


def _result(provider: ProviderName = ProviderName.DEEZER) -> SearchResult:
    return SearchResult(
        kind=ResultKind.TRACK,
        title="Let It Be",
        subtitle="The Beatles",
        image_url=None,
        confidence=Confidence.HIGH,
        sources=(SourceRef(provider=provider, external_id="1", url="https://x.example"),),
        extras={},
    )


@pytest.mark.unit
def test_in_memory_search_provider_satisfies_protocol() -> None:
    p = InMemorySearchProvider(name="deezer", canned=(_result(),))
    assert isinstance(p, SearchProvider)


@pytest.mark.unit
def test_in_memory_query_cache_satisfies_protocol() -> None:
    c = InMemoryQueryCache()
    assert isinstance(c, QueryCache)


@pytest.mark.unit
def test_in_memory_history_repo_satisfies_protocol() -> None:
    r = InMemorySearchHistoryRepository()
    assert isinstance(r, SearchHistoryRepository)


@pytest.mark.unit
def test_in_memory_click_repo_satisfies_protocol() -> None:
    r = InMemorySearchClickRepository()
    assert isinstance(r, SearchClickRepository)


@pytest.mark.unit
@pytest.mark.asyncio
async def test_in_memory_search_provider_returns_canned_results() -> None:
    canned = (_result(),)
    p = InMemorySearchProvider(name="deezer", canned=canned)
    resp = await p.search("the beatles", frozenset({ResultKind.TRACK}), 25)
    assert resp.provider_name == "deezer"
    assert resp.status is ProviderStatus.OK
    assert resp.results == canned
    assert resp.latency_ms == 0


@pytest.mark.unit
@pytest.mark.asyncio
async def test_in_memory_history_repo_trims_to_n_on_explicit_call() -> None:
    repo = InMemorySearchHistoryRepository()
    # Insert 5 entries; trim_to_n(2) leaves the latest 2.
    for i in range(5):
        await repo.insert(
            SearchHistoryEntry(
                id=SearchHistoryEntryId(uuid4()),
                user_id=_USER,
                query=f"q{i}",
                query_norm=f"q{i}",
                executed_at=_NOW + timedelta(seconds=i),
                result_clicked_signature=None,
            )
        )
    await repo.trim_to_n(_USER, 2)
    listed = await repo.list_distinct_recent(_USER, limit=10)
    assert len(listed) == 2
    # Latest two queries by executed_at are q4 then q3.
    assert [e.query for e in listed] == ["q4", "q3"]


@pytest.mark.unit
@pytest.mark.asyncio
async def test_in_memory_click_repo_dedupes_within_60s_of_last_persist() -> None:
    repo = InMemorySearchClickRepository()
    sig = "track:let-it-be:beatles"
    base = _NOW
    first = SearchClick(
        id=SearchClickId(uuid4()),
        user_id=_USER,
        query_norm="beatles",
        result_signature=sig,
        position=0,
        confidence=Confidence.HIGH,
        clicked_at=base,
    )
    second = SearchClick(
        id=SearchClickId(uuid4()),
        user_id=_USER,
        query_norm="beatles",
        result_signature=sig,
        position=0,
        confidence=Confidence.HIGH,
        clicked_at=base + timedelta(seconds=30),
    )
    third = SearchClick(
        id=SearchClickId(uuid4()),
        user_id=_USER,
        query_norm="beatles",
        result_signature=sig,
        position=0,
        confidence=Confidence.HIGH,
        clicked_at=base + timedelta(seconds=120),
    )
    o1 = await repo.insert_if_outside_window(first, window_seconds=60)
    o2 = await repo.insert_if_outside_window(second, window_seconds=60)
    o3 = await repo.insert_if_outside_window(third, window_seconds=60)
    assert isinstance(o1, ClickInsertOutcome)
    assert o1.inserted and o1.deduped_against_id is None
    assert not o2.inserted and o2.deduped_against_id == first.id.value
    assert o3.inserted and o3.deduped_against_id is None
