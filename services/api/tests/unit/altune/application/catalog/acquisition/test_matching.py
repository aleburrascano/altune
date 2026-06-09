"""Gate-based match verification — the accuracy core of acquire-track."""

from __future__ import annotations

import pytest
from altune.application.catalog.acquisition.matching import (
    artist_gate,
    duration_gate,
    select_best_candidate,
    title_gate,
)

from altune.application.catalog.ports import AudioCandidate


@pytest.mark.unit
def test_title_gate_accepts_exact_match() -> None:
    assert title_gate("Blinding Lights", "Blinding Lights") is True


@pytest.mark.unit
def test_title_gate_accepts_case_insensitive() -> None:
    assert title_gate("blinding lights", "BLINDING LIGHTS") is True


@pytest.mark.unit
def test_title_gate_rejects_below_085() -> None:
    assert title_gate("Blinding Lights", "Bohemian Rhapsody") is False


@pytest.mark.unit
def test_title_gate_strips_feat_brackets() -> None:
    assert title_gate("Song (feat. Artist B)", "Song") is True


@pytest.mark.unit
def test_artist_gate_accepts_exact_match() -> None:
    assert artist_gate("The Weeknd", "The Weeknd") is True


@pytest.mark.unit
def test_artist_gate_accepts_partial_match() -> None:
    assert artist_gate("The Weeknd", "Weeknd") is True


@pytest.mark.unit
def test_artist_gate_rejects_different_artist() -> None:
    assert artist_gate("The Weeknd", "John Legend") is False


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
def test_select_best_candidate_picks_highest_title_jw() -> None:
    good = AudioCandidate(
        title="Blinding Lights", artist="The Weeknd", duration_seconds=210, url="http://a"
    )
    ok = AudioCandidate(
        title="Blinding Light", artist="The Weeknd", duration_seconds=210, url="http://b"
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
        title="Blinding Lights", artist="The Weeknd", duration_seconds=500, url="http://wrong"
    )
    right = AudioCandidate(
        title="Blinding Lights", artist="The Weeknd", duration_seconds=212, url="http://right"
    )
    result = select_best_candidate(
        track_title="Blinding Lights",
        track_artist="The Weeknd",
        track_duration=210,
        candidates=[wrong_duration, right],
    )
    assert result is not None
    assert result.url == "http://right"
