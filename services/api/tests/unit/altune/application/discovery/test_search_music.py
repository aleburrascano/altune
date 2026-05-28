"""SearchMusic use case — slice 16 spine + scatter-gather tests for later slices."""

from __future__ import annotations

import asyncio
from datetime import UTC, datetime
from uuid import UUID

import pytest
from tests._doubles.in_memory_search_history_repository import (
    InMemorySearchHistoryRepository,
)
from tests._doubles.in_memory_search_provider import InMemorySearchProvider

from altune.application.discovery.ports import ProviderSearchResponse
from altune.application.discovery.search_music import SearchMusic, SearchMusicInput
from altune.domain.discovery.confidence import Confidence
from altune.domain.discovery.provider import ProviderName
from altune.domain.discovery.provider_status import ProviderStatus
from altune.domain.discovery.result_kind import ResultKind
from altune.domain.discovery.search_result import SearchResult
from altune.domain.discovery.source_ref import SourceRef
from altune.domain.shared.user_id import UserId

_USER = UserId(UUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))


def _result(
    provider: ProviderName,
    title: str = "Let It Be",
    ext_id: str = "1",
    isrc: str | None = None,
) -> SearchResult:
    extras: dict[str, object] = {}
    if isrc is not None:
        extras["isrc"] = isrc
    return SearchResult(
        kind=ResultKind.TRACK,
        title=title,
        subtitle="The Beatles",
        image_url=None,
        confidence=Confidence.LOW,
        sources=(SourceRef(provider=provider, external_id=ext_id, url=f"https://x/{ext_id}"),),
        extras=extras,
    )


@pytest.mark.unit
@pytest.mark.asyncio
async def test_search_music_returns_dedup_ranked_results_and_persists_history() -> None:
    provider = InMemorySearchProvider(name="deezer", canned=(_result(ProviderName.DEEZER),))
    history = InMemorySearchHistoryRepository()
    use_case = SearchMusic(providers=[provider], history_repo=history)

    output = await use_case.execute(
        SearchMusicInput(
            raw_query="the beatles",
            user_id=_USER,
            kinds=frozenset({ResultKind.TRACK}),
        )
    )

    assert output.query == "the beatles"
    assert output.query_norm == "beatles"
    assert len(output.results) == 1
    assert output.providers[0].provider_name == "deezer"
    assert output.providers[0].status is ProviderStatus.OK
    assert output.partial is False

    listed = await history.list_distinct_recent(_USER, limit=10)
    assert len(listed) == 1


class _SlowProvider:
    name = "slow"

    async def search(
        self,
        query: str,
        kinds: frozenset[ResultKind],
        limit: int,
    ) -> ProviderSearchResponse:
        _ = (query, kinds, limit)
        await asyncio.sleep(3.0)
        return ProviderSearchResponse(
            provider_name=self.name, status=ProviderStatus.OK, results=(), latency_ms=0
        )

    async def lookup_by_url(self, url: str) -> SearchResult | None:
        _ = url
        return None


@pytest.mark.unit
@pytest.mark.asyncio
async def test_search_music_returns_partial_on_one_provider_timeout() -> None:
    ok = InMemorySearchProvider(name="deezer", canned=(_result(ProviderName.DEEZER),))
    slow = _SlowProvider()
    history = InMemorySearchHistoryRepository()
    use_case = SearchMusic(
        providers=[ok, slow], history_repo=history, per_source_timeout_s=0.05
    )

    output = await use_case.execute(
        SearchMusicInput(
            raw_query="the beatles",
            user_id=_USER,
            kinds=frozenset({ResultKind.TRACK}),
        )
    )

    assert output.partial is True
    statuses = {s.provider_name: s.status for s in output.providers}
    assert statuses["deezer"] is ProviderStatus.OK
    assert statuses["slow"] is ProviderStatus.TIMEOUT


class _ErrorProvider:
    name = "broken"

    async def search(
        self,
        query: str,
        kinds: frozenset[ResultKind],
        limit: int,
    ) -> ProviderSearchResponse:
        _ = (query, kinds, limit)
        raise RuntimeError("provider blew up")

    async def lookup_by_url(self, url: str) -> SearchResult | None:
        _ = url
        return None


@pytest.mark.unit
@pytest.mark.asyncio
async def test_search_music_maps_raised_exception_to_error_status() -> None:
    broken = _ErrorProvider()
    history = InMemorySearchHistoryRepository()
    use_case = SearchMusic(providers=[broken], history_repo=history)

    output = await use_case.execute(
        SearchMusicInput(
            raw_query="the beatles",
            user_id=_USER,
            kinds=frozenset({ResultKind.TRACK}),
        )
    )

    assert output.partial is True
    assert output.providers[0].status is ProviderStatus.ERROR


@pytest.mark.unit
@pytest.mark.asyncio
async def test_search_music_persists_history_even_on_partial_response() -> None:
    broken = _ErrorProvider()
    history = InMemorySearchHistoryRepository()
    use_case = SearchMusic(providers=[broken], history_repo=history)

    await use_case.execute(
        SearchMusicInput(
            raw_query="the beatles",
            user_id=_USER,
            kinds=frozenset({ResultKind.TRACK}),
        )
    )
    listed = await history.list_distinct_recent(_USER, limit=10)
    assert len(listed) == 1


class _FailingHistory:
    async def insert(self, entry: object) -> None:
        _ = entry
        raise RuntimeError("db down")

    async def trim_to_n(self, user_id: object, n: int) -> None:
        _ = (user_id, n)

    async def list_distinct_recent(self, user_id: object, limit: int) -> tuple[object, ...]:
        _ = (user_id, limit)
        return ()


@pytest.mark.unit
@pytest.mark.asyncio
async def test_search_music_returns_200_when_history_insert_raises() -> None:
    provider = InMemorySearchProvider(name="deezer", canned=(_result(ProviderName.DEEZER),))
    use_case = SearchMusic(providers=[provider], history_repo=_FailingHistory())  # type: ignore[arg-type]

    output = await use_case.execute(
        SearchMusicInput(
            raw_query="the beatles",
            user_id=_USER,
            kinds=frozenset({ResultKind.TRACK}),
        )
    )
    # Search still succeeded; history failure didn't propagate.
    assert len(output.results) == 1
    assert output.providers[0].status is ProviderStatus.OK


_NOW = datetime(2026, 5, 27, 12, 0, tzinfo=UTC)
