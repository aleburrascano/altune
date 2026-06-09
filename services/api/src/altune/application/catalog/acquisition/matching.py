"""Three-tier source selection for audio acquisition.

Tier 1: ISRC deterministic lookup (handled by SearchStep)
Tier 2: Structured metadata ranking (this module)
Tier 3: Best-effort identity match (this module)

No keyword banks. No title parsing heuristics. Ranking uses YouTube's
own structured metadata fields (channel type, category, duration,
view count) instead of scanning titles for "audio"/"video"/"clean".

See docs/specs/acquire-track/design-v2-source-selection.md
"""

from __future__ import annotations

from typing import TYPE_CHECKING

import structlog
from rapidfuzz import fuzz

from altune.application.discovery.normalize import normalize_for_match

if TYPE_CHECKING:
    from altune.application.catalog.ports import AudioCandidate

_logger = structlog.get_logger(__name__)

_IDENTITY_MIN = 60
_DURATION_TIGHT = 3
_DURATION_LOOSE = 15


def identity_score(track_title: str, track_artist: str, candidate_title: str) -> float:
    combined = normalize_for_match(f"{track_artist} {track_title}")
    title_only = normalize_for_match(track_title)
    candidate_norm = normalize_for_match(candidate_title)
    return max(
        fuzz.token_sort_ratio(combined, candidate_norm),
        fuzz.token_sort_ratio(title_only, candidate_norm),
    )


def _channel_score(channel: str) -> float:
    """Score by channel type. Topic channels are auto-generated official audio."""
    if channel.endswith("- Topic"):
        return 1.0
    lower = channel.lower()
    if "vevo" in lower:
        return 0.8
    return 0.3


def _category_score(categories: tuple[str, ...]) -> float:
    if "Music" in categories:
        return 1.0
    return 0.2


def _duration_score(expected: int | None, actual: int | None) -> float:
    if expected is None or actual is None:
        return 0.5
    diff = abs(expected - actual)
    if diff <= _DURATION_TIGHT:
        return 1.0
    if diff <= _DURATION_LOOSE:
        return 0.5
    return 0.0


def _view_score(view_count: int, max_views: int) -> float:
    if max_views == 0:
        return 0.5
    return min(view_count / max_views, 1.0)


def metadata_rank(
    candidate: AudioCandidate,
    expected_duration: int | None,
    max_views: int,
) -> float:
    """Composite metadata quality score (0.0 - 1.0). No keyword parsing."""
    ch = _channel_score(candidate.channel)
    cat = _category_score(candidate.categories)
    dur = _duration_score(expected_duration, candidate.duration_seconds)
    views = _view_score(candidate.view_count, max_views)
    return 0.45 * ch + 0.25 * dur + 0.20 * cat + 0.10 * views


def _is_topic_channel(channel: str) -> bool:
    return channel.endswith("- Topic")


def select_best_candidate(
    *,
    track_title: str,
    track_artist: str,
    track_duration: int | None,
    candidates: list[AudioCandidate],
) -> AudioCandidate | None:
    """Three-tier selection: Topic channel first, metadata rank second, identity fallback third."""
    if not candidates:
        return None

    max_views = max((c.view_count for c in candidates), default=0)

    topic_candidates: list[tuple[float, AudioCandidate]] = []
    other_candidates: list[tuple[float, float, AudioCandidate]] = []

    for c in candidates:
        ident = identity_score(track_title, track_artist, c.title)
        meta = metadata_rank(c, track_duration, max_views)
        _logger.info(
            "candidate_evaluated",
            candidate_title=c.title,
            candidate_channel=c.channel,
            candidate_duration=c.duration_seconds,
            candidate_views=c.view_count,
            identity_score=round(ident, 1),
            metadata_rank=round(meta, 3),
            is_topic=_is_topic_channel(c.channel),
        )
        if ident < _IDENTITY_MIN:
            continue
        if _is_topic_channel(c.channel):
            topic_candidates.append((ident, c))
        else:
            other_candidates.append((meta, ident, c))

    if topic_candidates:
        topic_candidates.sort(key=lambda x: x[0], reverse=True)
        best = topic_candidates[0][1]
        _logger.info("candidate_selected", title=best.title, channel=best.channel, source="topic_channel")
        return best

    if other_candidates:
        other_candidates.sort(key=lambda x: (x[0], x[1]), reverse=True)
        best = other_candidates[0][2]
        _logger.info(
            "candidate_selected", title=best.title, channel=best.channel,
            metadata_rank=round(other_candidates[0][0], 3), source="metadata_rank",
        )
        return best

    _logger.warning(
        "no_candidates_passed",
        track_title=track_title,
        track_artist=track_artist,
        total_candidates=len(candidates),
    )
    return None
