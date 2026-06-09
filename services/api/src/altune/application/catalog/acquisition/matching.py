"""Gate-based match verification for audio acquisition.

Three hard pass/fail gates — no tunable thresholds, no weighted composite.
Per the acquire-track design doc: gates were chosen over scoring because a
candidate can't compensate for a wrong title with a correct duration.
"""

from __future__ import annotations

from typing import TYPE_CHECKING

from rapidfuzz.distance import JaroWinkler

from altune.application.discovery.normalize import normalize_for_match

if TYPE_CHECKING:
    from altune.application.catalog.ports import AudioCandidate

_TITLE_THRESHOLD = 0.85
_ARTIST_THRESHOLD = 0.70
_DURATION_TOLERANCE_SECONDS = 15


def title_gate(track_title: str, candidate_title: str) -> bool:
    a = normalize_for_match(track_title)
    b = normalize_for_match(candidate_title)
    return JaroWinkler.normalized_similarity(a, b) >= _TITLE_THRESHOLD


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
    for c in candidates:
        if not title_gate(track_title, c.title):
            continue
        if not artist_gate(track_artist, c.artist):
            continue
        if not duration_gate(track_duration, c.duration_seconds):
            continue
        score = JaroWinkler.normalized_similarity(
            normalize_for_match(track_title),
            normalize_for_match(c.title),
        )
        passing.append((score, c))
    if not passing:
        return None
    passing.sort(key=lambda x: x[0], reverse=True)
    return passing[0][1]
