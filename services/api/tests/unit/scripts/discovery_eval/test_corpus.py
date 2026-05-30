"""Unit tests for the discovery-eval corpus generator (pure functions)."""

from __future__ import annotations

import pytest
from scripts.discovery_eval.corpus import (
    EvalQuery,
    LibraryTrack,
    build_corpus,
    messy,
    typo,
    variants_for,
)

pytestmark = pytest.mark.unit


def _track(
    title: str = "Africa",
    artist: str = "Toto",
    album: str | None = "Toto IV",
) -> LibraryTrack:
    return LibraryTrack(title=title, artist=artist, album=album, album_artist=artist, genre="Rock")


def _by_category(track: LibraryTrack) -> dict[str, EvalQuery]:
    return {q.category: q for q in variants_for(track, source="library")}


def test_track_exact_carries_title_and_artist_label() -> None:
    q = _by_category(_track())["track_exact"]
    assert q.query == "Africa Toto"
    assert q.expected_kind == "track"
    assert q.expected_title == "Africa"
    assert q.expected_subtitle == "Toto"


def test_artist_only_expects_artist_kind_with_no_subtitle() -> None:
    q = _by_category(_track())["artist_only"]
    assert q.query == "Toto"
    assert q.expected_kind == "artist"
    assert q.expected_title == "Toto"
    assert q.expected_subtitle is None


def test_album_variant_expects_album_kind() -> None:
    q = _by_category(_track())["album"]
    assert q.expected_kind == "album"
    assert q.expected_title == "Toto IV"
    assert q.expected_subtitle == "Toto"


def test_album_variant_skipped_when_track_has_no_album() -> None:
    cats = _by_category(_track(album=None))
    assert "album" not in cats


def test_messy_lowercases_and_strips_punctuation() -> None:
    assert messy("M.A.A.D City (feat. MF DOOM)!") == "maad city feat mf doom"


def test_typo_is_deterministic_and_changes_one_char() -> None:
    out_a = typo("radiohead")
    out_b = typo("radiohead")
    assert out_a == out_b  # deterministic, no RNG seed drift
    assert out_a != "radiohead"
    assert len(out_a) == len("radiohead")


def test_partial_skipped_for_short_titles() -> None:
    # A one-word title can't yield a meaningful partial distinct from track_only.
    cats = _by_category(_track(title="Monster"))
    assert "partial" not in cats


def test_partial_uses_leading_words_for_long_titles() -> None:
    q = _by_category(_track(title="Bohemian Rhapsody Reprise", artist="Queen"))["partial"]
    assert q.query.startswith("Bohemian Rhapsody")
    assert q.query.endswith("Queen")
    assert q.expected_title == "Bohemian Rhapsody Reprise"


def test_build_corpus_respects_max_queries_cap() -> None:
    tracks = [_track(title=f"Song {i}", artist=f"Artist {i}") for i in range(100)]
    corpus = build_corpus(tracks, max_queries=30, seed=7)
    assert len(corpus) <= 30


def test_build_corpus_is_deterministic_for_a_seed() -> None:
    tracks = [_track(title=f"Song {i}", artist=f"Artist {i}") for i in range(40)]
    first = [q.query for q in build_corpus(tracks, max_queries=25, seed=11)]
    second = [q.query for q in build_corpus(tracks, max_queries=25, seed=11)]
    assert first == second


def test_build_corpus_spans_categories_when_tracks_exceed_cap() -> None:
    # Regression: with far more tracks than the cap, the corpus must still span
    # multiple categories — not be exhausted by track_exact (one per track).
    tracks = [
        _track(title=f"Song {i} Two Three", artist=f"Artist {i}", album=f"Album {i}")
        for i in range(500)
    ]
    corpus = build_corpus(tracks, max_queries=21, seed=3)
    categories = {q.category for q in corpus}
    assert len(categories) >= 5
