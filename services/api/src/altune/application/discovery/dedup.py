"""dedup_and_rank — ISRC + JW merge + multi-criteria ranking.

Per ADR-0007 + discover-music-v1 spec §3.3 + §3.13. Pure function over
a sequence of SearchResults. ISRC-match collapses entries to HIGH; JW
>= 0.92 on normalized (artist|title) collapses to HIGH; JW in
[0.85, 0.92) collapses to MEDIUM; otherwise entries stay separate
(as LOW). Per-source priors break ties and choose canonical
representative; final ordering is confidence DESC -> multi-source bool
DESC -> winning-prior DESC -> alpha (subtitle, title).
"""

from __future__ import annotations

from typing import TYPE_CHECKING

from rapidfuzz.distance import JaroWinkler  # type: ignore[import-not-found,unused-ignore]

from altune.application.discovery.normalize import normalize_for_match
from altune.domain.discovery.confidence import Confidence
from altune.domain.discovery.provider import ProviderName
from altune.domain.discovery.search_result import SearchResult

if TYPE_CHECKING:
    from collections.abc import Sequence

    from altune.domain.discovery.source_ref import SourceRef

# Per-source priors per ADR-0007 §"per-source priors".
_PRIORS: dict[ProviderName, float] = {
    ProviderName.MUSICBRAINZ: 0.95,
    ProviderName.DEEZER: 0.85,
    ProviderName.LASTFM: 0.80,
    ProviderName.SOUNDCLOUD: 0.65,
}

_JW_HIGH = 0.92
_JW_MEDIUM = 0.85


def _isrc_of(result: SearchResult) -> str | None:
    val = result.extras.get("isrc")
    if isinstance(val, str) and val:
        return val
    return None


def _winning_prior(result: SearchResult) -> float:
    return max(_PRIORS.get(s.provider, 0.0) for s in result.sources)


def _signature(result: SearchResult) -> str:
    """Normalized (artist|title) key for JW comparison."""
    title = normalize_for_match(result.title)
    subtitle = normalize_for_match(result.subtitle or "")
    return f"{subtitle}|{title}"


def _merge(a: SearchResult, b: SearchResult, confidence: Confidence) -> SearchResult:
    """Merge two results; higher per-source prior wins title/subtitle/extras."""
    canonical, other = (a, b) if _winning_prior(a) >= _winning_prior(b) else (b, a)
    # Dedup sources by (provider, external_id); preserve canonical's order.
    seen: set[tuple[ProviderName, str]] = set()
    sources: list[SourceRef] = []
    for s in (*canonical.sources, *other.sources):
        key = (s.provider, s.external_id)
        if key in seen:
            continue
        seen.add(key)
        sources.append(s)
    # Merge extras with canonical winning on conflict.
    extras = {**other.extras, **canonical.extras}
    return SearchResult(
        kind=canonical.kind,
        title=canonical.title,
        subtitle=canonical.subtitle,
        image_url=canonical.image_url or other.image_url,
        confidence=confidence,
        sources=tuple(sources),
        extras=extras,
    )


def _try_merge(a: SearchResult, b: SearchResult) -> SearchResult | None:
    """Return merged result, or None if they shouldn't merge."""
    if a.kind is not b.kind:
        return None
    isrc_a, isrc_b = _isrc_of(a), _isrc_of(b)
    if isrc_a and isrc_b and isrc_a == isrc_b:
        return _merge(a, b, Confidence.HIGH)
    sim = JaroWinkler.similarity(_signature(a), _signature(b))
    if sim >= _JW_HIGH:
        return _merge(a, b, Confidence.HIGH)
    if sim >= _JW_MEDIUM:
        return _merge(a, b, Confidence.MEDIUM)
    return None


def _as_low_confidence(result: SearchResult) -> SearchResult:
    """Standalone results carry LOW confidence; merging overrides via _merge."""
    if result.confidence is Confidence.LOW:
        return result
    return SearchResult(
        kind=result.kind,
        title=result.title,
        subtitle=result.subtitle,
        image_url=result.image_url,
        confidence=Confidence.LOW,
        sources=result.sources,
        extras=result.extras,
    )


def _rank_key(result: SearchResult) -> tuple[int, int, float, str, str]:
    confidence_rank = {Confidence.HIGH: 2, Confidence.MEDIUM: 1, Confidence.LOW: 0}[
        result.confidence
    ]
    multi_source = 1 if len(result.sources) > 1 else 0
    prior = _winning_prior(result)
    # Negate so higher values sort FIRST when ascending.
    return (-confidence_rank, -multi_source, -prior, result.subtitle or "", result.title)


def dedup_and_rank(results: Sequence[SearchResult]) -> tuple[SearchResult, ...]:
    """Merge ISRC- or JW-similar entries; rank per ADR-0007 §3.13."""
    if not results:
        return ()
    merged: list[SearchResult] = []
    for incoming in results:
        candidate = _as_low_confidence(incoming)
        absorbed = False
        for i, existing in enumerate(merged):
            attempt = _try_merge(existing, candidate)
            if attempt is not None:
                merged[i] = attempt
                absorbed = True
                break
        if not absorbed:
            merged.append(candidate)
    return tuple(sorted(merged, key=_rank_key))
