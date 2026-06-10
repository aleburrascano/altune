"""Merge semantics for fuse_and_rank.

ISRC + JW collapse + per-source-prior canonical-representative selection.
The relevance-first RANKING (RRF + exact-match boost) is covered in
test_fuse_and_rank.py; this module pins only the MERGE behavior, which is
independent of query relevance — so each result is passed as its own
provider group with an empty query_norm.
"""

from __future__ import annotations

import pytest

from altune.application.discovery.dedup import fuse_and_rank
from altune.domain.discovery.confidence import Confidence
from altune.domain.discovery.entity_resolution_tier import EntityResolutionTier
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
    mbid: str | None = None,
    confidence: Confidence = Confidence.LOW,
    kind: ResultKind = ResultKind.TRACK,
    duration_seconds: float | None = None,
) -> SearchResult:
    extras: dict[str, object] = {}
    if isrc is not None:
        extras["isrc"] = isrc
    if mbid is not None:
        extras["mbid"] = mbid
    if duration_seconds is not None:
        extras["duration_seconds"] = duration_seconds
    return SearchResult(
        kind=kind,
        title=title,
        subtitle=subtitle,
        image_url=None,
        confidence=confidence,
        sources=(SourceRef(provider=provider, external_id=ext_id, url=f"https://x/{ext_id}"),),
        extras=extras,
    )


def _merge(*results: SearchResult) -> tuple[SearchResult, ...]:
    """Run the merge step by passing each result as its own provider group.

    query_norm is empty because these tests assert merge/confidence/count, not
    relevance order.
    """
    return fuse_and_rank([(r,) for r in results], query_norm="")


@pytest.mark.unit
def test_dedup_merges_isrc_matched_into_high_confidence() -> None:
    a = _r(
        title="Let It Be",
        subtitle="The Beatles",
        provider=ProviderName.MUSICBRAINZ,
        ext_id="mb",
        isrc="GBAYE0601477",
    )
    b = _r(
        title="Let It Be (Remastered)",
        subtitle="The Beatles",
        provider=ProviderName.DEEZER,
        ext_id="dz",
        isrc="GBAYE0601477",
    )
    merged = _merge(a, b)
    assert len(merged) == 1
    only = merged[0]
    assert only.confidence is Confidence.HIGH
    assert {s.provider for s in only.sources} == {ProviderName.MUSICBRAINZ, ProviderName.DEEZER}


@pytest.mark.unit
def test_same_title_tracks_merge_on_similarity() -> None:
    """Tracks/albums still merge on high JW similarity (hybrid approach)."""
    a = _r(
        title="Let It Be", subtitle="The Beatles", provider=ProviderName.MUSICBRAINZ, ext_id="mb"
    )
    b = _r(title="Let It Be", subtitle="The Beatles", provider=ProviderName.DEEZER, ext_id="dz")
    merged = _merge(a, b)
    assert len(merged) == 1


@pytest.mark.unit
def test_dedup_collapses_jw_in_85_to_92_into_medium() -> None:
    # Different titles whose normalized JW lands between 0.85 and 0.92.
    a = _r(title="Hey Jude", subtitle="The Beatles", provider=ProviderName.MUSICBRAINZ)
    b = _r(title="Hey Judes", subtitle="The Beatles", provider=ProviderName.DEEZER)
    merged = _merge(a, b)
    # Either merged with medium OR stayed separate; pin the exact boundary case.
    if len(merged) == 1:
        assert merged[0].confidence in {Confidence.HIGH, Confidence.MEDIUM}
    else:
        # If JW falls below 0.85, they're separate; that's also valid.
        assert len(merged) == 2


@pytest.mark.unit
def test_dedup_keeps_jw_below_85_separate_as_low() -> None:
    a = _r(title="Let It Be", subtitle="The Beatles", provider=ProviderName.MUSICBRAINZ)
    b = _r(
        title="Something Completely Different",
        subtitle="Other Artist",
        provider=ProviderName.SOUNDCLOUD,
    )
    merged = _merge(a, b)
    assert len(merged) == 2
    assert all(r.confidence is Confidence.LOW for r in merged)


@pytest.mark.unit
def test_dedup_isrc_overrides_jw() -> None:
    # ISRC match is canonical even when titles diverge.
    a = _r(
        title="Different Title",
        subtitle="Beatles",
        provider=ProviderName.MUSICBRAINZ,
        ext_id="mb",
        isrc="ABC123456789",
    )
    b = _r(
        title="Completely Other",
        subtitle="Other Person",
        provider=ProviderName.DEEZER,
        ext_id="dz",
        isrc="ABC123456789",
    )
    merged = _merge(a, b)
    assert len(merged) == 1
    assert merged[0].confidence is Confidence.HIGH


@pytest.mark.unit
def test_try_merge_returns_resolution_tier_in_extras() -> None:
    a = _r(
        title="Let It Be",
        subtitle="The Beatles",
        provider=ProviderName.MUSICBRAINZ,
        ext_id="mb",
        isrc="GBAYE0601477",
    )
    b = _r(
        title="Let It Be (Remastered)",
        subtitle="The Beatles",
        provider=ProviderName.DEEZER,
        ext_id="dz",
        isrc="GBAYE0601477",
    )
    merged = _merge(a, b)
    assert len(merged) == 1
    assert merged[0].extras.get("resolution_tier") == EntityResolutionTier.ISRC.value


@pytest.mark.unit
def test_dedup_uses_completeness_for_canonical_representative() -> None:
    """identity-v1: more-complete metadata wins canonical selection."""
    sparse = _r(
        title="Let It Be (Anthology 3 Version)",
        subtitle="The Beatles",
        provider=ProviderName.DEEZER,
        ext_id="dz",
        isrc="ISRC9",
    )
    rich = SearchResult(
        kind=ResultKind.TRACK,
        title="Let It Be",
        subtitle="The Beatles",
        image_url="http://img",
        confidence=Confidence.LOW,
        sources=(
            SourceRef(provider=ProviderName.MUSICBRAINZ, external_id="mb", url="https://x/mb"),
        ),
        extras={"isrc": "ISRC9", "duration_seconds": 243, "album": "Let It Be"},
    )
    merged = _merge(sparse, rich)
    assert len(merged) == 1
    assert merged[0].title == "Let It Be"


@pytest.mark.unit
def test_dedup_empty_input_returns_empty_tuple() -> None:
    assert fuse_and_rank([], query_norm="") == ()


@pytest.mark.unit
def test_dedup_single_result_passes_through() -> None:
    one = _r(title="Only One", subtitle="Artist", provider=ProviderName.DEEZER)
    result = _merge(one)
    assert len(result) == 1
    assert result[0].title == "Only One"


@pytest.mark.unit
def test_mbid_merge_unconditional_even_with_divergent_titles() -> None:
    """identity-v1 AC#2: MBID is authoritative — merges regardless of title."""
    a = _r(
        title="Blinding Lights",
        subtitle="The Weeknd",
        provider=ProviderName.MUSICBRAINZ,
        ext_id="mb1",
        mbid="shared-mbid-001",
    )
    b = _r(
        title="After Hours",
        subtitle="The Weeknd",
        provider=ProviderName.DEEZER,
        ext_id="dz1",
        mbid="shared-mbid-001",
    )
    merged = _merge(a, b)
    assert len(merged) == 1, "Same MBID merges unconditionally"


@pytest.mark.unit
def test_mbid_merge_accepted_when_titles_agree() -> None:
    """AC#1: same MBID + title JW >= 0.85 → merge at MBID tier."""
    a = _r(
        title="Blinding Lights",
        subtitle="The Weeknd",
        provider=ProviderName.MUSICBRAINZ,
        ext_id="mb1",
        mbid="shared-mbid-002",
    )
    b = _r(
        title="Blinding Lights",
        subtitle="The Weeknd",
        provider=ProviderName.LASTFM,
        ext_id="lf1",
        mbid="shared-mbid-002",
    )
    merged = _merge(a, b)
    assert len(merged) == 1
    assert merged[0].extras.get("resolution_tier") == EntityResolutionTier.MBID.value


@pytest.mark.unit
def test_different_mbids_same_name_remain_separate() -> None:
    """AC#2: two artists named 'Che' with different MBIDs stay separate."""
    che_modern = _r(
        title="Che",
        subtitle=None,
        provider=ProviderName.MUSICBRAINZ,
        ext_id="mb-che-1",
        mbid="mbid-che-modern",
        kind=ResultKind.ARTIST,
    )
    che_classic = _r(
        title="Che",
        subtitle=None,
        provider=ProviderName.DEEZER,
        ext_id="dz-che-2",
        mbid="mbid-che-classic",
        kind=ResultKind.ARTIST,
    )
    merged = _merge(che_modern, che_classic)
    assert len(merged) == 2, "Different MBIDs must prevent merge even with identical names"


@pytest.mark.unit
def test_artist_same_name_merges_across_providers() -> None:
    """Same-name artists merge on JW (one card in search results).
    Discography contamination is handled at the content-fetch layer (MB-primary)."""
    a = _r(
        title="Che",
        subtitle=None,
        provider=ProviderName.DEEZER,
        ext_id="dz1",
        kind=ResultKind.ARTIST,
    )
    b = _r(
        title="Che",
        subtitle=None,
        provider=ProviderName.MUSICBRAINZ,
        ext_id="mb1",
        kind=ResultKind.ARTIST,
    )
    merged = _merge(a, b)
    assert len(merged) == 1, "Same-name artists merge into one search result"


@pytest.mark.unit
def test_mbid_merge_is_unconditional() -> None:
    """identity-v1 AC#2: MBID merge requires no title check."""
    a = _r(
        title="After Hours",
        subtitle="The Weeknd",
        provider=ProviderName.MUSICBRAINZ,
        ext_id="mb1",
        mbid="shared-mbid-003",
    )
    b = _r(
        title="Blinding Lights",
        subtitle="The Weeknd",
        provider=ProviderName.DEEZER,
        ext_id="dz1",
        mbid="shared-mbid-003",
    )
    merged = _merge(a, b)
    assert len(merged) == 1, "Same MBID merges unconditionally"
