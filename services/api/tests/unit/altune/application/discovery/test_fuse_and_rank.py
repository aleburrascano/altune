"""fuse_and_rank — relevance-first ranking via RRF + exact-match boost.

The merge semantics (ISRC / JW collapse, canonical representative) are the
same as the legacy ranker and are covered in test_dedup.py. These tests pin
the NEW behavior: relevance to the query drives order, confidence does not,
and provider-native rank is fused across lists.
"""

from __future__ import annotations

import pytest

from altune.application.discovery.dedup import fuse_and_rank
from altune.domain.discovery.confidence import Confidence
from altune.domain.discovery.provider import ProviderName
from altune.domain.discovery.result_kind import ResultKind
from altune.domain.discovery.search_result import SearchResult
from altune.domain.discovery.source_ref import SourceRef


def _r(
    *,
    title: str,
    subtitle: str,
    provider: ProviderName,
    ext_id: str = "1",
    isrc: str | None = None,
) -> SearchResult:
    extras: dict[str, object] = {}
    if isrc is not None:
        extras["isrc"] = isrc
    return SearchResult(
        kind=ResultKind.TRACK,
        title=title,
        subtitle=subtitle,
        image_url=None,
        confidence=Confidence.LOW,
        sources=(SourceRef(provider=provider, external_id=ext_id, url=f"https://x/{ext_id}"),),
        extras=extras,
    )


@pytest.mark.unit
def test_query_relevant_single_source_beats_irrelevant_multi_source() -> None:
    # The exact match is single-source; an irrelevant entry is multi-source.
    # Relevance must win — this is the core user-reported bug.
    canonical = _r(title="Creep", subtitle="Radiohead", provider=ProviderName.DEEZER, ext_id="d")
    noise_a = _r(title="Creep", subtitle="TLC", provider=ProviderName.LASTFM, ext_id="l")
    noise_b = _r(title="Creep", subtitle="TLC", provider=ProviderName.MUSICBRAINZ, ext_id="m")
    ranked = fuse_and_rank([(canonical,), (noise_a,), (noise_b,)], query_norm="creep radiohead")
    assert ranked[0].subtitle == "Radiohead"


@pytest.mark.unit
def test_exact_match_beats_alphabetical_tiebreak() -> None:
    # Same title, several artists; only one matches the query's artist token.
    # Legacy ranker sorted these alphabetically by subtitle; relevance wins now.
    toto_d = _r(title="Africa", subtitle="TOTO", provider=ProviderName.DEEZER, ext_id="d")
    toto_l = _r(title="Africa", subtitle="TOTO", provider=ProviderName.LASTFM, ext_id="l")
    other = _r(title="Africa", subtitle="Angelique Kidjo", provider=ProviderName.DEEZER, ext_id="o")
    ranked = fuse_and_rank([(toto_d, other), (toto_l,)], query_norm="africa toto")
    assert ranked[0].subtitle == "TOTO"


@pytest.mark.unit
def test_rrf_rewards_provider_native_top_rank() -> None:
    # With equal query relevance, the result a provider ranked higher wins.
    # Distinct titles so they don't merge.
    top = _r(title="Bohemian Rhapsody", subtitle="Queen", provider=ProviderName.DEEZER, ext_id="a")
    lower = _r(title="Radio Gaga", subtitle="Queen", provider=ProviderName.DEEZER, ext_id="b")
    ranked = fuse_and_rank([(top, lower)], query_norm="queen")
    assert [r.title for r in ranked] == ["Bohemian Rhapsody", "Radio Gaga"]


@pytest.mark.unit
def test_multi_source_agreement_outranks_single_source_at_equal_relevance() -> None:
    # Both results match the query equally; the one multiple providers agree on
    # accrues more RRF and ranks first. Distinct titles so they stay separate.
    agreed_d = _r(
        title="Bohemian Rhapsody", subtitle="Queen", provider=ProviderName.DEEZER, ext_id="d"
    )
    agreed_l = _r(
        title="Bohemian Rhapsody", subtitle="Queen", provider=ProviderName.LASTFM, ext_id="l"
    )
    solo = _r(title="Radio Gaga", subtitle="Queen", provider=ProviderName.SOUNDCLOUD, ext_id="s")
    ranked = fuse_and_rank([(agreed_d,), (agreed_l,), (solo,)], query_norm="queen")
    assert ranked[0].title == "Bohemian Rhapsody"
    assert len(ranked[0].sources) == 2


@pytest.mark.unit
def test_relevance_floor_drops_zero_relevance_results() -> None:
    # A result with no token overlap with the query must be excluded entirely.
    match = _r(title="Creep", subtitle="Radiohead", provider=ProviderName.DEEZER, ext_id="d")
    junk = _r(title="Under Pressure", subtitle="Queen", provider=ProviderName.ITUNES, ext_id="i")
    ranked = fuse_and_rank([(match,), (junk,)], query_norm="creep radiohead")
    titles = [r.title for r in ranked]
    assert "Creep" in titles
    assert "Under Pressure" not in titles


@pytest.mark.unit
def test_graded_relevance_more_query_tokens_ranks_higher() -> None:
    # 3-of-4 query tokens must outrank 2-of-4 — the discrimination a binary
    # all-tokens-present boost could not provide.
    more = _r(title="Rest in the Bass", subtitle="FARNOISE", provider=ProviderName.MUSICBRAINZ)
    fewer = _r(title="Rest In", subtitle="Cyborg Project", provider=ProviderName.MUSICBRAINZ)
    ranked = fuse_and_rank([(more,), (fewer,)], query_norm="che rest in bass")
    assert ranked[0].title == "Rest in the Bass"


@pytest.mark.unit
def test_artist_only_query_surfaces_artist_track() -> None:
    # An artist-only query must clear the floor via the artist (subtitle) match,
    # not be dropped because the title carries extra tokens.
    track = _r(title="Bohemian Rhapsody", subtitle="Queen", provider=ProviderName.DEEZER)
    ranked = fuse_and_rank([(track,)], query_norm="queen")
    assert len(ranked) == 1
    assert ranked[0].subtitle == "Queen"


def _pop_result(*, title: str, popularity: float, ext_id: str) -> SearchResult:
    return SearchResult(
        kind=ResultKind.TRACK,
        title=title,
        subtitle="Queen",
        image_url=None,
        confidence=Confidence.LOW,
        sources=(SourceRef(provider=ProviderName.DEEZER, external_id=ext_id, url=f"https://x/{ext_id}"),),
        extras={"popularity": popularity},
    )


@pytest.mark.unit
def test_popularity_outranks_agreement_within_a_relevance_band() -> None:
    # Both match the query ("queen") equally (band 1.0 via artist). The obscure
    # one sits at the provider's rank 0 (better RRF); the popular one at rank 1.
    # Popularity is above RRF in the sort key, so the popular one still wins.
    obscure = _pop_result(title="Radio Ga Ga", popularity=0.1, ext_id="b")
    popular = _pop_result(title="Bohemian Rhapsody", popularity=0.9, ext_id="a")
    ranked = fuse_and_rank([(obscure, popular)], query_norm="queen")
    assert ranked[0].title == "Bohemian Rhapsody"


@pytest.mark.unit
def test_artist_headlines_over_equally_relevant_track() -> None:
    # An artist-name query: the artist and a track by that artist both match at
    # band 1.0; kind-priority (Artist > Track) makes the artist the Top Result.
    artist = SearchResult(
        kind=ResultKind.ARTIST,
        title="Queen",
        subtitle=None,
        image_url=None,
        confidence=Confidence.LOW,
        sources=(SourceRef(provider=ProviderName.DEEZER, external_id="ar", url="https://x/ar"),),
        extras={},
    )
    track = _r(title="Bohemian Rhapsody", subtitle="Queen", provider=ProviderName.DEEZER, ext_id="t")
    ranked = fuse_and_rank([(track, artist)], query_norm="queen")
    assert ranked[0].kind is ResultKind.ARTIST


@pytest.mark.unit
def test_empty_input_returns_empty_tuple() -> None:
    assert fuse_and_rank([], query_norm="anything") == ()
