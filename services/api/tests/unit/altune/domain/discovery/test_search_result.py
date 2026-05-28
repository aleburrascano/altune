"""SearchResult aggregate — slice 6 of discover-music-v1.

Per AC#1 (wire result-entry shape) + AC#3 (multi-source merge carries
multiple sources). Invariants: title non-empty, sources non-empty.
"""

from __future__ import annotations

import pytest
from altune.domain.discovery.search_result import SearchResult

from altune.domain.discovery.confidence import Confidence
from altune.domain.discovery.provider import ProviderName
from altune.domain.discovery.result_kind import ResultKind
from altune.domain.discovery.source_ref import SourceRef


def _src(provider: ProviderName, ext_id: str = "x", url: str = "https://x.example") -> SourceRef:
    return SourceRef(provider=provider, external_id=ext_id, url=url)


@pytest.mark.unit
def test_search_result_rejects_empty_title() -> None:
    with pytest.raises(ValueError, match="title"):
        SearchResult(
            kind=ResultKind.TRACK,
            title="",
            subtitle="The Beatles",
            image_url=None,
            confidence=Confidence.HIGH,
            sources=(_src(ProviderName.DEEZER),),
            extras={},
        )


@pytest.mark.unit
def test_search_result_rejects_empty_sources_tuple() -> None:
    with pytest.raises(ValueError, match="sources"):
        SearchResult(
            kind=ResultKind.TRACK,
            title="Let It Be",
            subtitle="The Beatles",
            image_url=None,
            confidence=Confidence.HIGH,
            sources=(),
            extras={},
        )


@pytest.mark.unit
def test_search_result_equals_by_value() -> None:
    a = SearchResult(
        kind=ResultKind.TRACK,
        title="Let It Be",
        subtitle="The Beatles",
        image_url=None,
        confidence=Confidence.HIGH,
        sources=(_src(ProviderName.DEEZER, "1"),),
        extras={"isrc": "GBAYE0601477"},
    )
    b = SearchResult(
        kind=ResultKind.TRACK,
        title="Let It Be",
        subtitle="The Beatles",
        image_url=None,
        confidence=Confidence.HIGH,
        sources=(_src(ProviderName.DEEZER, "1"),),
        extras={"isrc": "GBAYE0601477"},
    )
    assert a == b


@pytest.mark.unit
def test_search_result_carries_multi_source_tuple() -> None:
    sources = (
        _src(ProviderName.MUSICBRAINZ, "mb-id"),
        _src(ProviderName.DEEZER, "1"),
    )
    r = SearchResult(
        kind=ResultKind.TRACK,
        title="Let It Be",
        subtitle="The Beatles",
        image_url=None,
        confidence=Confidence.HIGH,
        sources=sources,
        extras={},
    )
    assert r.sources == sources
    assert len(r.sources) == 2
