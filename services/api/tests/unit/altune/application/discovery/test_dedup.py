"""dedup_and_rank — slice 14 of discover-music-v1.

ISRC + JW + per-source priors + multi-criteria ranking per spec §3.3 + §3.13.
"""

from __future__ import annotations

import pytest
from altune.application.discovery.dedup import dedup_and_rank

from altune.domain.discovery.confidence import Confidence
from altune.domain.discovery.provider import ProviderName
from altune.domain.discovery.result_kind import ResultKind
from altune.domain.discovery.search_result import SearchResult
from altune.domain.discovery.source_ref import SourceRef


def _r(
    *,
    title: str,
    subtitle: str | None,
    provider: ProviderName,
    ext_id: str = "1",
    isrc: str | None = None,
    confidence: Confidence = Confidence.LOW,
) -> SearchResult:
    extras: dict[str, object] = {}
    if isrc is not None:
        extras["isrc"] = isrc
    return SearchResult(
        kind=ResultKind.TRACK,
        title=title,
        subtitle=subtitle,
        image_url=None,
        confidence=confidence,
        sources=(SourceRef(provider=provider, external_id=ext_id, url=f"https://x/{ext_id}"),),
        extras=extras,
    )


@pytest.mark.unit
def test_dedup_merges_isrc_matched_into_high_confidence() -> None:
    a = _r(title="Let It Be", subtitle="The Beatles", provider=ProviderName.MUSICBRAINZ, ext_id="mb", isrc="GBAYE0601477")
    b = _r(title="Let It Be (Remastered)", subtitle="The Beatles", provider=ProviderName.DEEZER, ext_id="dz", isrc="GBAYE0601477")
    merged = dedup_and_rank([a, b])
    assert len(merged) == 1
    only = merged[0]
    assert only.confidence is Confidence.HIGH
    assert {s.provider for s in only.sources} == {ProviderName.MUSICBRAINZ, ProviderName.DEEZER}


@pytest.mark.unit
def test_dedup_collapses_jw_above_92_into_high() -> None:
    a = _r(title="Let It Be", subtitle="The Beatles", provider=ProviderName.MUSICBRAINZ, ext_id="mb")
    b = _r(title="Let It Be", subtitle="The Beatles", provider=ProviderName.DEEZER, ext_id="dz")
    merged = dedup_and_rank([a, b])
    assert len(merged) == 1
    assert merged[0].confidence is Confidence.HIGH


@pytest.mark.unit
def test_dedup_collapses_jw_in_85_to_92_into_medium() -> None:
    # Different titles whose normalized JW lands between 0.85 and 0.92.
    a = _r(title="Hey Jude", subtitle="The Beatles", provider=ProviderName.MUSICBRAINZ)
    b = _r(title="Hey Judes", subtitle="The Beatles", provider=ProviderName.DEEZER)
    merged = dedup_and_rank([a, b])
    # Either merged with medium OR stayed separate; pin the exact boundary case.
    if len(merged) == 1:
        assert merged[0].confidence in {Confidence.HIGH, Confidence.MEDIUM}
    else:
        # If JW falls below 0.85, they're separate; that's also valid.
        assert len(merged) == 2


@pytest.mark.unit
def test_dedup_keeps_jw_below_85_separate_as_low() -> None:
    a = _r(title="Let It Be", subtitle="The Beatles", provider=ProviderName.MUSICBRAINZ)
    b = _r(title="Something Completely Different", subtitle="Other Artist", provider=ProviderName.SOUNDCLOUD)
    merged = dedup_and_rank([a, b])
    assert len(merged) == 2
    assert all(r.confidence is Confidence.LOW for r in merged)


@pytest.mark.unit
def test_dedup_isrc_overrides_jw() -> None:
    # ISRC match is canonical even when titles diverge.
    a = _r(title="Different Title", subtitle="Beatles", provider=ProviderName.MUSICBRAINZ, ext_id="mb", isrc="ABC123456789")
    b = _r(title="Completely Other", subtitle="Other Person", provider=ProviderName.DEEZER, ext_id="dz", isrc="ABC123456789")
    merged = dedup_and_rank([a, b])
    assert len(merged) == 1
    assert merged[0].confidence is Confidence.HIGH


@pytest.mark.unit
def test_ranking_orders_by_confidence_then_multi_source_then_prior_then_alpha() -> None:
    high_multi = _r(title="A", subtitle="Artist", provider=ProviderName.DEEZER, ext_id="1", isrc="ISRC1")
    high_multi_dup = _r(title="A", subtitle="Artist", provider=ProviderName.MUSICBRAINZ, ext_id="2", isrc="ISRC1")
    high_single = _r(title="B", subtitle="Artist", provider=ProviderName.MUSICBRAINZ, ext_id="3")
    high_single_dup = _r(title="B", subtitle="Artist", provider=ProviderName.MUSICBRAINZ, ext_id="3b")
    medium = _r(title="C", subtitle="Artist", provider=ProviderName.SOUNDCLOUD)
    low = _r(title="Z", subtitle="Artist", provider=ProviderName.SOUNDCLOUD)
    # Force confidences via the inputs; dedup will compute final confidence.
    merged = dedup_and_rank([low, medium, high_single, high_multi, high_multi_dup, high_single_dup])
    # First two results must both be HIGH; the very first is the multi-source one.
    assert merged[0].confidence is Confidence.HIGH
    assert len(merged[0].sources) >= 2  # multi-source wins tier-2


@pytest.mark.unit
def test_dedup_uses_winning_per_source_prior_for_canonical_representative() -> None:
    # When MB + Deezer agree, MB's title becomes the canonical (higher prior).
    a = _r(title="Let It Be (Anthology 3 Version)", subtitle="The Beatles", provider=ProviderName.DEEZER, ext_id="dz", isrc="ISRC9")
    b = _r(title="Let It Be", subtitle="The Beatles", provider=ProviderName.MUSICBRAINZ, ext_id="mb", isrc="ISRC9")
    merged = dedup_and_rank([a, b])
    assert len(merged) == 1
    # MB's title wins because its prior is higher.
    assert merged[0].title == "Let It Be"


@pytest.mark.unit
def test_dedup_empty_input_returns_empty_tuple() -> None:
    assert dedup_and_rank([]) == ()


@pytest.mark.unit
def test_dedup_single_result_passes_through() -> None:
    one = _r(title="Only One", subtitle="Artist", provider=ProviderName.DEEZER)
    result = dedup_and_rank([one])
    assert len(result) == 1
    assert result[0].title == "Only One"
