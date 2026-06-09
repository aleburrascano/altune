"""Combined-identity match verification for audio acquisition.

Uses token_sort_ratio on combined identity strings (artist + title) instead
of field-by-field JW gates. This handles YouTube's "Artist - Title (Qualifier)
ft. Guest" format naturally — token_sort_ratio is order-independent, so all
the right words being present is what matters, not their position.

See docs/solutions/design-patterns/2026-06-08-combined-identity-string-matching-over-field-gates.md
"""

from __future__ import annotations

from typing import TYPE_CHECKING

import structlog
from rapidfuzz import fuzz

from altune.application.discovery.normalize import normalize_for_match

if TYPE_CHECKING:
    from altune.application.catalog.ports import AudioCandidate

_logger = structlog.get_logger(__name__)

_IDENTITY_THRESHOLD = 70
_DURATION_TOLERANCE_SECONDS = 15

_AUDIO_KEYWORDS = {"audio", "official audio"}
_VIDEO_KEYWORDS = {"video", "music video", "official video", "visualizer", "mv"}


def identity_score(track_title: str, track_artist: str, candidate_title: str) -> float:
    """Score a candidate against the track's combined identity.

    Tries two forms and takes the max:
    1. Combined: "artist title" vs candidate (catches YouTube's "Artist - Title" format)
    2. Title-only: "title" vs candidate (catches cases where artist is in a separate field)
    """
    combined = normalize_for_match(f"{track_artist} {track_title}")
    title_only = normalize_for_match(track_title)
    candidate_norm = normalize_for_match(candidate_title)
    return max(
        fuzz.token_sort_ratio(combined, candidate_norm),
        fuzz.token_sort_ratio(title_only, candidate_norm),
    )


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
    """Score all candidates by identity match, return the best above threshold."""
    passing: list[tuple[float, AudioCandidate]] = []
    for c in candidates:
        score = identity_score(track_title, track_artist, c.title)
        d_pass = duration_gate(track_duration, c.duration_seconds)
        _logger.info(
            "candidate_evaluated",
            candidate_title=c.title,
            candidate_artist=c.artist,
            candidate_duration=c.duration_seconds,
            identity_score=round(score, 1),
            duration_pass=d_pass,
        )
        if score < _IDENTITY_THRESHOLD:
            continue
        if not d_pass:
            continue
        passing.append((score, c))
    if not passing:
        _logger.warning(
            "no_candidates_passed",
            track_title=track_title,
            track_artist=track_artist,
            total_candidates=len(candidates),
        )
        return None
    passing.sort(key=lambda x: (_audio_preference(x[1].title), x[0]), reverse=True)
    return passing[0][1]


def _audio_preference(title: str) -> int:
    """Prefer audio-only uploads over music videos. 1=audio, 0=neutral, -1=video."""
    lower = title.lower()
    if any(kw in lower for kw in _AUDIO_KEYWORDS):
        return 1
    if any(kw in lower for kw in _VIDEO_KEYWORDS):
        return -1
    return 0
