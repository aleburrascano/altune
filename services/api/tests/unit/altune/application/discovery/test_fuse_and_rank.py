"""fuse_and_rank — relevance-first ranking via RRF + exact-match boost.

The merge semantics (ISRC / JW collapse, canonical representative) are the
same as the legacy ranker and are covered in test_dedup.py. These tests pin
the NEW behavior: relevance to the query drives order, confidence does not,
and provider-native rank is fused across lists.
"""

from __future__ import annotations

import pytest

from altune.application.discovery.dedup import fuse_and_rank
from altune.application.discovery.quality_scorer import compute_quality_score
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
    # Equal relevance (same title => band 1.0 via title match), distinct artists
    # so they don't merge. The one the provider ranked higher wins on RRF.
    top = _r(title="Anthem", subtitle="Alpha", provider=ProviderName.DEEZER, ext_id="a")
    lower = _r(title="Anthem", subtitle="Omega", provider=ProviderName.DEEZER, ext_id="b")
    ranked = fuse_and_rank([(top, lower)], query_norm="anthem")
    assert [r.subtitle for r in ranked] == ["Alpha", "Omega"]


def _pop(result: SearchResult, popularity: float) -> SearchResult:
    return SearchResult(
        kind=result.kind,
        title=result.title,
        subtitle=result.subtitle,
        image_url=result.image_url,
        confidence=result.confidence,
        sources=result.sources,
        extras={**result.extras, "popularity": popularity},
    )


@pytest.mark.unit
def test_record_type_demotion_sinks_compilation_below_album() -> None:
    """identity-v1 AC#8: non-canonical record_type is demoted within band."""
    real = _pop(
        SearchResult(
            kind=ResultKind.TRACK,
            title="Blinding Lights",
            subtitle="The Weeknd",
            image_url=None,
            confidence=Confidence.LOW,
            sources=(SourceRef(provider=ProviderName.DEEZER, external_id="g", url="https://x/g"),),
            extras={"record_type": "album"},
        ),
        0.5,
    )
    comp = _pop(
        SearchResult(
            kind=ResultKind.TRACK,
            title="Blinding Lights",
            subtitle="Various",
            image_url=None,
            confidence=Confidence.LOW,
            sources=(SourceRef(provider=ProviderName.ITUNES, external_id="c", url="https://x/c"),),
            extras={"record_type": "compilation"},
        ),
        0.9,
    )
    ranked = fuse_and_rank([(comp, real)], query_norm="blinding lights")
    assert ranked[0].subtitle == "The Weeknd"


@pytest.mark.unit
def test_identifier_merge_creates_multi_source() -> None:
    """With identifier-only merge, ISRC match still creates multi-source results."""
    a = _r(title="Anthem", subtitle="Alpha", provider=ProviderName.DEEZER, ext_id="d", isrc="ISRC1")
    b = _r(title="Anthem", subtitle="Alpha", provider=ProviderName.LASTFM, ext_id="l", isrc="ISRC1")
    solo = _r(title="Anthem", subtitle="Omega", provider=ProviderName.SOUNDCLOUD, ext_id="s")
    ranked = fuse_and_rank([(a,), (b,), (solo,)], query_norm="anthem")
    merged = [r for r in ranked if len(r.sources) == 2]
    assert len(merged) == 1
    assert merged[0].subtitle == "Alpha"


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


def _pop_result(*, title: str, subtitle: str, popularity: float, ext_id: str) -> SearchResult:
    return SearchResult(
        kind=ResultKind.TRACK,
        title=title,
        subtitle=subtitle,
        image_url=None,
        confidence=Confidence.LOW,
        sources=(
            SourceRef(provider=ProviderName.DEEZER, external_id=ext_id, url=f"https://x/{ext_id}"),
        ),
        extras={"popularity": popularity},
    )


@pytest.mark.unit
def test_popularity_outranks_agreement_within_a_relevance_band() -> None:
    # Both match equally (band 1.0 via the shared title), distinct artists so no
    # merge. The obscure one sits at provider rank 0 (better RRF); the popular one
    # at rank 1. Popularity is above RRF in the sort key, so the popular one wins.
    obscure = _pop_result(title="Anthem", subtitle="Omega", popularity=0.1, ext_id="b")
    popular = _pop_result(title="Anthem", subtitle="Alpha", popularity=0.9, ext_id="a")
    ranked = fuse_and_rank([(obscure, popular)], query_norm="anthem")
    assert ranked[0].subtitle == "Alpha"


@pytest.mark.unit
def test_more_popular_match_headlines_regardless_of_kind() -> None:
    # A song query: an obscure artist whose NAME equals the query must not
    # headline over the popular song. Best relevance x popularity wins, any kind.
    obscure_artist = SearchResult(
        kind=ResultKind.ARTIST,
        title="Creep",
        subtitle=None,
        image_url=None,
        confidence=Confidence.LOW,
        sources=(SourceRef(provider=ProviderName.DEEZER, external_id="ar", url="https://x/ar"),),
        extras={"popularity": 0.2},
    )
    popular_song = SearchResult(
        kind=ResultKind.TRACK,
        title="Creep",
        subtitle="Radiohead",
        image_url=None,
        confidence=Confidence.LOW,
        sources=(SourceRef(provider=ProviderName.DEEZER, external_id="tr", url="https://x/tr"),),
        extras={"popularity": 0.95},
    )
    ranked = fuse_and_rank([(obscure_artist,), (popular_song,)], query_norm="creep")
    assert ranked[0].kind is ResultKind.TRACK
    assert ranked[0].subtitle == "Radiohead"


@pytest.mark.unit
def test_empty_input_returns_empty_tuple() -> None:
    assert fuse_and_rank([], query_norm="anything") == ()


@pytest.mark.unit
def test_compilation_record_type_demoted_within_band() -> None:
    """identity-v1 AC#8: compilation record_type is demoted below album."""
    real = SearchResult(
        kind=ResultKind.ALBUM,
        title="Anthem",
        subtitle="The Band",
        image_url=None,
        confidence=Confidence.LOW,
        sources=(SourceRef(provider=ProviderName.DEEZER, external_id="r", url="https://x/r"),),
        extras={"record_type": "album"},
    )
    comp = SearchResult(
        kind=ResultKind.ALBUM,
        title="Anthem",
        subtitle="Various",
        image_url=None,
        confidence=Confidence.LOW,
        sources=(SourceRef(provider=ProviderName.MUSICBRAINZ, external_id="c", url="https://x/c"),),
        extras={"record_type": "Compilation"},
    )
    ranked = fuse_and_rank([(comp,), (real,)], query_norm="anthem")
    assert ranked[0].subtitle == "The Band"


@pytest.mark.unit
def test_quality_score_replaces_prior_in_sort_key() -> None:
    """AC#10: rich-metadata result from low-prior provider outranks sparse
    result from high-prior provider when quality_scorer is passed."""
    rich_sc = SearchResult(
        kind=ResultKind.TRACK,
        title="Track",
        subtitle="Artist",
        image_url="http://img",
        confidence=Confidence.LOW,
        sources=(
            SourceRef(provider=ProviderName.SOUNDCLOUD, external_id="sc1", url="https://sc/1"),
        ),
        extras={"isrc": "ISRC1", "duration_seconds": 200, "album": "Album"},
    )
    sparse_mb = SearchResult(
        kind=ResultKind.TRACK,
        title="Track",
        subtitle="Artist",
        image_url=None,
        confidence=Confidence.LOW,
        sources=(
            SourceRef(provider=ProviderName.MUSICBRAINZ, external_id="mb1", url="https://mb/1"),
        ),
        extras={},
    )
    ranked = fuse_and_rank(
        [(rich_sc,), (sparse_mb,)],
        query_norm="track artist",
        quality_scorer=compute_quality_score,
    )
    assert ranked[0].extras.get("isrc") == "ISRC1", (
        "Rich-metadata result should rank above sparse when using quality scorer"
    )


@pytest.mark.unit
def test_album_name_deterministic_regardless_of_provider_order() -> None:
    """AC#19: same query with providers in different order → same album name."""

    def _track_with_album(
        album: str,
        provider: ProviderName,
        ext_id: str,
        isrc: str,
        image_url: str | None = None,
    ) -> SearchResult:
        return SearchResult(
            kind=ResultKind.TRACK,
            title="Song",
            subtitle="Artist",
            image_url=image_url,
            confidence=Confidence.LOW,
            sources=(SourceRef(provider=provider, external_id=ext_id, url=f"https://x/{ext_id}"),),
            extras={"isrc": isrc, "album": album},
        )

    dz = _track_with_album(
        "Real Album", ProviderName.DEEZER, "dz1", "ISRC1", image_url="http://img"
    )
    mb = _track_with_album("Compilation: Greatest Hits", ProviderName.MUSICBRAINZ, "mb1", "ISRC1")

    order_a = fuse_and_rank([(dz,), (mb,)], query_norm="song artist")
    order_b = fuse_and_rank([(mb,), (dz,)], query_norm="song artist")
    assert order_a[0].extras.get("album") == order_b[0].extras.get("album"), (
        "Album name must be deterministic regardless of provider response order"
    )


@pytest.mark.unit
def test_single_char_query_does_not_match_everything() -> None:
    """AC#20: query 'X' must not gate in 'Bohemian Rhapsody'."""
    x_track = _r(title="X", subtitle="Ed Sheeran", provider=ProviderName.DEEZER, ext_id="d1")
    unrelated = _r(
        title="Bohemian Rhapsody", subtitle="Queen", provider=ProviderName.DEEZER, ext_id="d2"
    )
    ranked = fuse_and_rank([(x_track, unrelated)], query_norm="x")
    titles = [r.title for r in ranked]
    assert "X" in titles
    assert "Bohemian Rhapsody" not in titles, (
        "Single-char query 'x' must not gate in unrelated results"
    )


@pytest.mark.unit
def test_rerank_preserves_rrf_signal() -> None:
    """AC#21: RRF signal is preserved through rerank after enrichment."""
    from altune.application.discovery.dedup import rerank

    high_rrf = SearchResult(
        kind=ResultKind.TRACK,
        title="Song",
        subtitle="Artist A",
        image_url=None,
        confidence=Confidence.LOW,
        sources=(
            SourceRef(provider=ProviderName.SOUNDCLOUD, external_id="sc1", url="https://x/sc1"),
        ),
        extras={"popularity": 0.3, "_rrf": 0.05},
    )
    # Low RRF but higher prior (MB=0.95 > SC=0.65) — without RRF in the sort
    # key, this result would win on prior alone.
    low_rrf = SearchResult(
        kind=ResultKind.TRACK,
        title="Song",
        subtitle="Artist B",
        image_url=None,
        confidence=Confidence.LOW,
        sources=(
            SourceRef(provider=ProviderName.MUSICBRAINZ, external_id="mb1", url="https://x/mb1"),
        ),
        extras={"popularity": 0.3, "_rrf": 0.001},
    )
    ranked = rerank([low_rrf, high_rrf], query_norm="song")
    assert ranked[0].subtitle == "Artist A", (
        "High-RRF result should rank above low-RRF even when low-RRF has higher prior"
    )


@pytest.mark.unit
def test_no_static_priors_or_junk_markers_in_dedup_module() -> None:
    """AC#7: _PRIORS, _JUNK_TITLE_MARKERS, _STOPWORDS must not exist as module attrs."""
    import altune.application.discovery.dedup as dedup_mod

    for name in ("_PRIORS", "_JUNK_TITLE_MARKERS", "_STOPWORDS"):
        assert not hasattr(dedup_mod, name), f"{name} still exists as module-level constant"
