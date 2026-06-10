"""Tests for the quality scorer."""

from __future__ import annotations

import pytest

from altune.application.discovery.quality_scorer import compute_quality_score
from altune.domain.discovery.confidence import Confidence
from altune.domain.discovery.entity_resolution_tier import EntityResolutionTier
from altune.domain.discovery.provider import ProviderName
from altune.domain.discovery.result_kind import ResultKind
from altune.domain.discovery.search_result import SearchResult
from altune.domain.discovery.source_ref import SourceRef


def _result(
    *,
    providers: list[ProviderName] | None = None,
    isrc: str | None = None,
    image_url: str | None = None,
    duration: float | None = None,
    album: str | None = None,
    resolution_tier: str | None = None,
) -> SearchResult:
    if providers is None:
        providers = [ProviderName.DEEZER]
    extras: dict[str, object] = {}
    if isrc is not None:
        extras["isrc"] = isrc
    if duration is not None:
        extras["duration_seconds"] = duration
    if album is not None:
        extras["album"] = album
    if resolution_tier is not None:
        extras["resolution_tier"] = resolution_tier
    return SearchResult(
        kind=ResultKind.TRACK,
        title="Test Track",
        subtitle="Test Artist",
        image_url=image_url,
        confidence=Confidence.LOW,
        sources=tuple(
            SourceRef(provider=p, external_id=f"{p.value}-1", url=f"https://{p.value}/1")
            for p in providers
        ),
        extras=extras,
    )


@pytest.mark.unit
def test_multi_source_scores_higher_than_single_source() -> None:
    multi = _result(
        providers=[ProviderName.DEEZER, ProviderName.LASTFM, ProviderName.MUSICBRAINZ],
        isrc="ISRC1",
    )
    single = _result(providers=[ProviderName.DEEZER], isrc="ISRC1")
    multi_score = compute_quality_score(multi)
    single_score = compute_quality_score(single)
    assert multi_score.composite > single_score.composite


@pytest.mark.unit
def test_complete_metadata_scores_higher() -> None:
    complete = _result(isrc="ISRC1", image_url="http://img", duration=180.0, album="Album")
    sparse = _result()
    assert compute_quality_score(complete).completeness > compute_quality_score(sparse).completeness


@pytest.mark.unit
def test_mbid_tier_scores_higher_than_none() -> None:
    mbid = _result(resolution_tier=EntityResolutionTier.MBID.value)
    none = _result(resolution_tier=EntityResolutionTier.NONE.value)
    assert compute_quality_score(mbid).entity_tier > compute_quality_score(none).entity_tier


@pytest.mark.unit
def test_score_is_in_unit_interval() -> None:
    result = _result(
        providers=[ProviderName.DEEZER, ProviderName.LASTFM],
        isrc="ISRC1",
        image_url="http://img",
        duration=180.0,
        album="Album",
        resolution_tier=EntityResolutionTier.MBID.value,
    )
    score = compute_quality_score(result)
    assert 0.0 <= score.composite <= 1.0


@pytest.mark.unit
def test_sparse_single_source_scores_below_rich_multi_source() -> None:
    """AC#9: no-ISRC, no-image, single-source scores below rich multi-source."""
    sparse = _result()
    rich = _result(
        providers=[ProviderName.DEEZER, ProviderName.LASTFM, ProviderName.MUSICBRAINZ],
        isrc="ISRC1",
        image_url="http://img",
        duration=200.0,
        album="Album",
        resolution_tier=EntityResolutionTier.ISRC.value,
    )
    assert compute_quality_score(rich).composite > compute_quality_score(sparse).composite
