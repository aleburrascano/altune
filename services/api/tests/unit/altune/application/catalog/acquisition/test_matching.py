"""Combined-identity match verification — token_sort_ratio approach."""

from __future__ import annotations

import pytest

from altune.application.catalog.acquisition.matching import (
    duration_gate,
    identity_score,
    select_best_candidate,
)
from altune.application.catalog.ports import AudioCandidate


@pytest.mark.unit
def test_identity_score_exact_match() -> None:
    score = identity_score("Blinding Lights", "The Weeknd", "Blinding Lights")
    assert score >= 70


@pytest.mark.unit
def test_identity_score_youtube_artist_prefix() -> None:
    score = identity_score(
        "INTOXYCATED", "Oxlade", "Oxlade - INTOXYCATED (Lyrics video) ft. Dave"
    )
    assert score >= 70


@pytest.mark.unit
def test_identity_score_rejects_completely_different() -> None:
    score = identity_score("Blinding Lights", "The Weeknd", "Bohemian Rhapsody Queen")
    assert score < 70


@pytest.mark.unit
def test_identity_score_handles_official_audio_tag() -> None:
    score = identity_score(
        "Blinding Lights", "The Weeknd", "The Weeknd - Blinding Lights (Official Audio)"
    )
    assert score >= 70


@pytest.mark.unit
def test_identity_score_handles_music_video_tag() -> None:
    score = identity_score(
        "HUMBLE.", "Kendrick Lamar", "Kendrick Lamar - HUMBLE. (Music Video)"
    )
    assert score >= 70


@pytest.mark.unit
def test_duration_gate_accepts_within_tolerance() -> None:
    assert duration_gate(210, 215) is True


@pytest.mark.unit
def test_duration_gate_rejects_beyond_tolerance() -> None:
    assert duration_gate(210, 240) is False


@pytest.mark.unit
def test_duration_gate_skips_when_expected_is_none() -> None:
    assert duration_gate(None, 300) is True


@pytest.mark.unit
def test_duration_gate_skips_when_actual_is_none() -> None:
    assert duration_gate(200, None) is True


@pytest.mark.unit
def test_select_best_candidate_picks_highest_score() -> None:
    good = AudioCandidate(
        title="Blinding Lights", artist="The Weeknd", duration_seconds=210, url="http://a"
    )
    ok = AudioCandidate(
        title="Blinding Lights Remix", artist="DJ Someone", duration_seconds=210, url="http://b"
    )
    result = select_best_candidate(
        track_title="Blinding Lights",
        track_artist="The Weeknd",
        track_duration=210,
        candidates=[ok, good],
    )
    assert result is not None
    assert result.url == "http://a"


@pytest.mark.unit
def test_select_returns_none_when_all_fail() -> None:
    bad = AudioCandidate(
        title="Completely Wrong Song", artist="Other Person", duration_seconds=100, url="http://x"
    )
    result = select_best_candidate(
        track_title="Blinding Lights",
        track_artist="The Weeknd",
        track_duration=210,
        candidates=[bad],
    )
    assert result is None


@pytest.mark.unit
def test_select_returns_none_for_empty_candidates() -> None:
    result = select_best_candidate(
        track_title="Blinding Lights",
        track_artist="The Weeknd",
        track_duration=210,
        candidates=[],
    )
    assert result is None


@pytest.mark.unit
def test_select_filters_by_duration_gate() -> None:
    wrong_duration = AudioCandidate(
        title="Blinding Lights The Weeknd", artist="The Weeknd", duration_seconds=500, url="http://wrong"
    )
    right = AudioCandidate(
        title="Blinding Lights The Weeknd", artist="The Weeknd", duration_seconds=212, url="http://right"
    )
    result = select_best_candidate(
        track_title="Blinding Lights",
        track_artist="The Weeknd",
        track_duration=210,
        candidates=[wrong_duration, right],
    )
    assert result is not None
    assert result.url == "http://right"


@pytest.mark.unit
def test_select_accepts_youtube_format_intoxycated() -> None:
    candidate = AudioCandidate(
        title="Oxlade - INTOXYCATED (Lyrics video) ft. Dave",
        artist="Oxlade",
        duration_seconds=211,
        url="http://yt/intoxycated",
    )
    result = select_best_candidate(
        track_title="INTOXYCATED",
        track_artist="Oxlade",
        track_duration=211,
        candidates=[candidate],
    )
    assert result is not None
    assert result.url == "http://yt/intoxycated"
