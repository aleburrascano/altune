"""Gate-based match verification for audio acquisition.

Three hard pass/fail gates — no tunable thresholds, no weighted composite.
Per the acquire-track design doc: gates were chosen over scoring because a
candidate can't compensate for a wrong title with a correct duration.
"""

from __future__ import annotations

import re
from typing import TYPE_CHECKING

import structlog
from rapidfuzz.distance import JaroWinkler

from altune.application.discovery.normalize import normalize_for_match

if TYPE_CHECKING:
    from altune.application.catalog.ports import AudioCandidate

_logger = structlog.get_logger(__name__)

_TITLE_THRESHOLD = 0.85
_ARTIST_THRESHOLD = 0.70
_DURATION_TOLERANCE_SECONDS = 15

_ARTIST_TITLE_SEP = re.compile(r"^[^-–—]+ [-–—] ")
_TRAILING_FEAT = re.compile(r"\s+feat\s+.*$", re.IGNORECASE)


def _clean_youtube_title(title: str) -> str:
    """Strip common YouTube title patterns: 'Artist - Title (Qualifier) ft. X'."""
    cleaned = _ARTIST_TITLE_SEP.sub("", title)
    cleaned = _TRAILING_FEAT.sub("", cleaned)
    return cleaned.strip() or title


def title_gate(track_title: str, candidate_title: str) -> bool:
    a = normalize_for_match(track_title)
    b_raw = normalize_for_match(candidate_title)
    b_cleaned = normalize_for_match(_clean_youtube_title(candidate_title))
    return max(
        JaroWinkler.normalized_similarity(a, b_raw),
        JaroWinkler.normalized_similarity(a, b_cleaned),
    ) >= _TITLE_THRESHOLD


def artist_gate(track_artist: str, candidate_artist: str) -> bool:
    a = normalize_for_match(track_artist)
    b = normalize_for_match(candidate_artist)
    return JaroWinkler.normalized_similarity(a, b) >= _ARTIST_THRESHOLD


def duration_gate(expected: int | None, actual: int | None) -> bool:
    if expected is None or actual is None:
        return True
    return abs(expected - actual) <= _DURATION_TOLERANCE_SECONDS


def select_best_candidate(
    *,
    track_title: str,
    track_artist: str,
    track_duration: int | None,
    candidates: list[AudioCandidate],
) -> AudioCandidate | None:
    """Apply gates to all candidates, return the best passing one or None."""
    passing: list[tuple[float, AudioCandidate]] = []
    norm_title = normalize_for_match(track_title)
    norm_artist = normalize_for_match(track_artist)
    for c in candidates:
        c_norm_title = normalize_for_match(c.title)
        c_cleaned_title = normalize_for_match(_clean_youtube_title(c.title))
        c_norm_artist = normalize_for_match(c.artist)
        title_jw = max(
            JaroWinkler.normalized_similarity(norm_title, c_norm_title),
            JaroWinkler.normalized_similarity(norm_title, c_cleaned_title),
        )
        artist_jw = JaroWinkler.normalized_similarity(norm_artist, c_norm_artist)
        t_pass = title_jw >= _TITLE_THRESHOLD
        a_pass = artist_jw >= _ARTIST_THRESHOLD
        d_pass = duration_gate(track_duration, c.duration_seconds)
        _logger.info(
            "candidate_evaluated",
            candidate_title=c.title,
            candidate_artist=c.artist,
            candidate_duration=c.duration_seconds,
            title_jw=round(title_jw, 3),
            artist_jw=round(artist_jw, 3),
            title_pass=t_pass,
            artist_pass=a_pass,
            duration_pass=d_pass,
        )
        if not t_pass or not a_pass or not d_pass:
            continue
        passing.append((title_jw, c))
    if not passing:
        _logger.warning(
            "no_candidates_passed_gates",
            track_title=track_title,
            track_artist=track_artist,
            total_candidates=len(candidates),
        )
        return None
    passing.sort(key=lambda x: x[0], reverse=True)
    return passing[0][1]
