"""Junk-title + bootleg demotion — restored after the 5a066c1 rewrite dropped it.

A '(Lofi Version)' / karaoke / sped-up upload lands in the same relevance band
as the genuine track (normalize strips bracketed suffixes) and can carry a
HIGHER popularity value (mixed scales: Deezer-native rank survives when the
Last.fm getInfo back-fill misses). The demotion tiebreak is what keeps the
genuine recording on top. Regression: "super shy" ranked a lo-fi cover #1.
"""

from __future__ import annotations

import pytest

from altune.application.discovery.dedup import fuse_and_rank, rerank
from altune.domain.discovery.confidence import Confidence
from altune.domain.discovery.provider import ProviderName
from altune.domain.discovery.result_kind import ResultKind
from altune.domain.discovery.search_result import SearchResult
from altune.domain.discovery.source_ref import SourceRef


def _track(
    title: str, subtitle: str, provider: ProviderName, ext_id: str, pop: float
) -> SearchResult:
    return SearchResult(
        kind=ResultKind.TRACK,
        title=title,
        subtitle=subtitle,
        image_url=None,
        confidence=Confidence.LOW,
        sources=(SourceRef(provider=provider, external_id=ext_id, url=f"https://x/{ext_id}"),),
        extras={"popularity": pop},
    )


@pytest.mark.unit
def test_lofi_version_demoted_below_genuine_within_band() -> None:
    genuine = _track("Super Shy", "NewJeans", ProviderName.LASTFM, "g1", pop=0.75)
    lofi = _track("Super Shy (Lofi Version)", "soopa bunnie", ProviderName.DEEZER, "l1", pop=0.86)

    ranked = fuse_and_rank([[lofi], [genuine]], "super shy")

    assert ranked[0].subtitle == "NewJeans"


@pytest.mark.unit
def test_rerank_demotes_lofi_after_popularity_enrichment() -> None:
    genuine = _track("Super Shy", "NewJeans", ProviderName.LASTFM, "g1", pop=0.75)
    lofi = _track("Super Shy (Lofi Version)", "soopa bunnie", ProviderName.DEEZER, "l1", pop=0.86)

    ranked = rerank([lofi, genuine], "super shy")

    assert ranked[0].subtitle == "NewJeans"


@pytest.mark.unit
def test_bootleg_title_stuffed_reupload_demoted() -> None:
    """Re-upload cramming the artist into the title sinks below the genuine split."""
    genuine = _track("Blinding Lights", "The Weeknd", ProviderName.LASTFM, "g1", pop=0.5)
    bootleg = _track(
        "Blinding Lights The Weeknd", "Pancadao GD Som", ProviderName.DEEZER, "b1", pop=0.9
    )

    ranked = fuse_and_rank([[bootleg], [genuine]], "blinding lights weeknd")

    assert ranked[0].subtitle == "The Weeknd"


@pytest.mark.unit
def test_genuine_not_demoted_on_bare_title_query() -> None:
    """A real Song/Artist entry on a title-only search must never be flagged."""
    genuine = _track("Super Shy", "NewJeans", ProviderName.LASTFM, "g1", pop=0.75)
    other = _track("Super Shy", "Ironmouse", ProviderName.DEEZER, "o1", pop=0.41)

    ranked = fuse_and_rank([[other], [genuine]], "super shy")

    assert ranked[0].subtitle == "NewJeans"  # plain popularity tiebreak, no demotion


@pytest.mark.unit
def test_junk_markers_respect_word_boundaries() -> None:
    """'cover' must not fire inside 'Undercover'; explicit junk vocab must fire."""
    from altune.application.discovery.dedup import _is_junk_title

    assert not _is_junk_title(_track("Undercover", "Artist", ProviderName.DEEZER, "u1", pop=0.1))
    assert _is_junk_title(
        _track("Super Shy (Piano Cover)", "Pianist", ProviderName.DEEZER, "c1", pop=0.1)
    )
    assert _is_junk_title(
        _track("Super Shy - sped up", "Edit Hub", ProviderName.DEEZER, "s1", pop=0.1)
    )
    assert _is_junk_title(
        _track("Super Shy (Nightcore)", "Edit Hub", ProviderName.DEEZER, "n1", pop=0.1)
    )
