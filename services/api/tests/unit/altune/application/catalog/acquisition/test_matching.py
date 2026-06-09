"""Three-tier source selection — metadata-ranked matching."""

from __future__ import annotations

import pytest

from altune.application.catalog.acquisition.matching import (
    _category_score,
    _channel_score,
    _duration_score,
    identity_score,
    metadata_rank,
    select_best_candidate,
)
from altune.application.catalog.ports import AudioCandidate


def _candidate(
    *,
    title: str = "Song",
    artist: str = "Artist",
    duration: int | None = 200,
    url: str = "http://yt/test",
    channel: str = "Artist",
    categories: tuple[str, ...] = ("Music",),
    views: int = 1000,
) -> AudioCandidate:
    return AudioCandidate(
        title=title, artist=artist, duration_seconds=duration, url=url,
        channel=channel, categories=categories, view_count=views,
    )


@pytest.mark.unit
def test_identity_score_exact_match() -> None:
    assert identity_score("Blinding Lights", "The Weeknd", "Blinding Lights") >= 60


@pytest.mark.unit
def test_identity_score_youtube_format() -> None:
    assert identity_score(
        "INTOXYCATED", "Oxlade", "Oxlade - INTOXYCATED (Lyrics video) ft. Dave"
    ) >= 60


@pytest.mark.unit
def test_identity_score_rejects_completely_different() -> None:
    assert identity_score("Blinding Lights", "The Weeknd", "Bohemian Rhapsody Queen") < 60


@pytest.mark.unit
def test_channel_score_topic_highest() -> None:
    assert _channel_score("Oxlade - Topic") == 1.0


@pytest.mark.unit
def test_channel_score_vevo_high() -> None:
    assert _channel_score("OxladeVEVO") == 0.8


@pytest.mark.unit
def test_channel_score_regular_low() -> None:
    assert _channel_score("RandomUploader") == 0.3


@pytest.mark.unit
def test_category_score_music() -> None:
    assert _category_score(("Music",)) == 1.0


@pytest.mark.unit
def test_category_score_other() -> None:
    assert _category_score(("Entertainment",)) == 0.2


@pytest.mark.unit
def test_duration_score_exact() -> None:
    assert _duration_score(200, 201) == 1.0


@pytest.mark.unit
def test_duration_score_close() -> None:
    assert _duration_score(200, 210) == 0.5


@pytest.mark.unit
def test_duration_score_far() -> None:
    assert _duration_score(200, 250) == 0.0


@pytest.mark.unit
def test_duration_score_unknown() -> None:
    assert _duration_score(None, 200) == 0.5


@pytest.mark.unit
def test_select_prefers_topic_channel() -> None:
    topic = _candidate(
        title="Blinding Lights", channel="The Weeknd - Topic",
        url="http://topic", views=1000000,
    )
    vevo = _candidate(
        title="The Weeknd - Blinding Lights (Official Video)",
        channel="TheWeekndVEVO", url="http://vevo", views=5000000,
    )
    result = select_best_candidate(
        track_title="Blinding Lights",
        track_artist="The Weeknd",
        track_duration=200,
        candidates=[vevo, topic],
    )
    assert result is not None
    assert result.url == "http://topic"


@pytest.mark.unit
def test_select_prefers_exact_duration() -> None:
    exact = _candidate(title="Song Artist", duration=200, url="http://exact", views=100)
    close = _candidate(title="Song Artist", duration=210, url="http://close", views=100000)
    result = select_best_candidate(
        track_title="Song", track_artist="Artist",
        track_duration=200, candidates=[close, exact],
    )
    assert result is not None
    assert result.url == "http://exact"


@pytest.mark.unit
def test_select_returns_none_when_all_fail_identity() -> None:
    bad = _candidate(title="Completely Wrong Song", artist="Other Person")
    result = select_best_candidate(
        track_title="Blinding Lights", track_artist="The Weeknd",
        track_duration=200, candidates=[bad],
    )
    assert result is None


@pytest.mark.unit
def test_select_returns_none_for_empty() -> None:
    result = select_best_candidate(
        track_title="Blinding Lights", track_artist="The Weeknd",
        track_duration=200, candidates=[],
    )
    assert result is None


@pytest.mark.unit
def test_select_accepts_youtube_format_intoxycated() -> None:
    candidate = _candidate(
        title="Oxlade - INTOXYCATED (Lyrics video) ft. Dave",
        artist="Oxlade", duration=211, url="http://yt/intoxycated",
        channel="Oxlade - Topic", views=5000000,
    )
    result = select_best_candidate(
        track_title="INTOXYCATED", track_artist="Oxlade",
        track_duration=211, candidates=[candidate],
    )
    assert result is not None


@pytest.mark.unit
def test_metadata_rank_topic_music_exact_duration() -> None:
    c = _candidate(channel="Artist - Topic", categories=("Music",), duration=200, views=1000)
    rank = metadata_rank(c, expected_duration=200, max_views=1000)
    assert rank > 0.8


@pytest.mark.unit
def test_metadata_rank_fan_upload_low() -> None:
    c = _candidate(channel="RandomFan", categories=("People & Blogs",), duration=220, views=50)
    rank = metadata_rank(c, expected_duration=200, max_views=1000)
    assert rank < 0.4
