"""Uniform popularity basis — native provider scales must never compete.

The enrichment back-fills a UNIFORM popularity (Last.fm getInfo) onto top
results. When the lookup misses, the old behavior kept the result's native
provider popularity (e.g. Deezer rank) — an incomparable scale that could
outrank uniform values. Regression: "super shy" ranked a '(Lofi Version)'
(native 0.86) above NewJeans (uniform 0.75). After enrichment a result's
popularity is the uniform value or 0.0 — one basis, no keyword lists.
"""

from __future__ import annotations

from uuid import UUID

import pytest
from tests._doubles.in_memory_search_history_repository import (
    InMemorySearchHistoryRepository,
)
from tests._doubles.in_memory_search_provider import InMemorySearchProvider

from altune.application.discovery.search_music import SearchMusic, SearchMusicInput
from altune.domain.discovery.confidence import Confidence
from altune.domain.discovery.provider import ProviderName
from altune.domain.discovery.result_kind import ResultKind
from altune.domain.discovery.search_result import SearchResult
from altune.domain.discovery.source_ref import SourceRef
from altune.domain.shared.user_id import UserId

_USER = UserId(UUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))


class _MapPopularityResolver:
    """Returns the uniform value for known (title, subtitle) pairs, else None."""

    def __init__(self, known: dict[tuple[str, str | None], float]) -> None:
        self.known = known

    async def resolve_popularity(
        self, kind: ResultKind, title: str, subtitle: str | None
    ) -> float | None:
        _ = kind
        return self.known.get((title, subtitle))


def _track(title: str, subtitle: str, ext_id: str, native_pop: float) -> SearchResult:
    return SearchResult(
        kind=ResultKind.TRACK,
        title=title,
        subtitle=subtitle,
        image_url=None,
        confidence=Confidence.LOW,
        sources=(SourceRef(provider=ProviderName.DEEZER, external_id=ext_id, url=f"https://x/{ext_id}"),),
        extras={"popularity": native_pop},
    )


async def _search(results: tuple[SearchResult, ...], resolver: _MapPopularityResolver, query: str):
    provider = InMemorySearchProvider(name="deezer", canned=results)
    use_case = SearchMusic(
        providers=[provider],
        history_repo=InMemorySearchHistoryRepository(),
        popularity_resolver=resolver,
    )
    return await use_case.execute(
        SearchMusicInput(raw_query=query, user_id=_USER, kinds=frozenset({ResultKind.TRACK}))
    )


@pytest.mark.unit
@pytest.mark.asyncio
async def test_native_popularity_dropped_when_uniform_lookup_misses() -> None:
    track = _track("Super Shy (Lofi Version)", "soopa bunnie", "l1", native_pop=0.86)
    resolver = _MapPopularityResolver({})  # no Last.fm entry for the edit

    out = await _search((track,), resolver, "super shy")

    assert out.results[0].extras.get("popularity") == 0.0


@pytest.mark.unit
@pytest.mark.asyncio
async def test_uniform_popularity_replaces_native_on_hit() -> None:
    track = _track("Super Shy", "NewJeans", "g1", native_pop=0.99)
    resolver = _MapPopularityResolver({("Super Shy", "NewJeans"): 0.75})

    out = await _search((track,), resolver, "super shy")

    assert out.results[0].extras.get("popularity") == 0.75


@pytest.mark.unit
@pytest.mark.asyncio
async def test_uniform_basis_ranks_genuine_above_native_scaled_edit() -> None:
    """The 'super shy' regression: lofi edit with high NATIVE popularity must
    not outrank the genuine track's UNIFORM popularity."""
    genuine = _track("Super Shy", "NewJeans", "g1", native_pop=0.70)
    lofi = _track("Super Shy (Lofi Version)", "soopa bunnie", "l1", native_pop=0.86)
    resolver = _MapPopularityResolver({("Super Shy", "NewJeans"): 0.75})

    out = await _search((lofi, genuine), resolver, "super shy")

    assert out.results[0].subtitle == "NewJeans"
