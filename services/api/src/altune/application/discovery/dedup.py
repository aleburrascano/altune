"""Merge + rank discovery results — identifier-only merge.

Merge step: ISRC match or MBID match only. No text similarity, no duration
matching, no keyword heuristics. Provider IDs are authoritative.

Rank step: relevance-first (token_sort_ratio), then quality score, then
popularity, then RRF agreement, then alphabetical.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import TYPE_CHECKING

from rapidfuzz import fuzz  # type: ignore[import-not-found,unused-ignore]

from altune.application.discovery.normalize import normalize_for_match
from altune.application.discovery.quality_scorer import is_demoted
from altune.domain.discovery.confidence import Confidence
from altune.domain.discovery.entity_resolution_tier import EntityResolutionTier
from altune.domain.discovery.provider import ProviderName
from altune.domain.discovery.search_result import SearchResult

if TYPE_CHECKING:
    from collections.abc import Callable, Sequence

    from altune.domain.discovery.quality_score import QualityScore
    from altune.domain.discovery.source_ref import SourceRef

    QualityScorer = Callable[[SearchResult], QualityScore]

_RRF_K = 60


def _isrc_of(result: SearchResult) -> str | None:
    val = result.extras.get("isrc")
    if isinstance(val, str) and val:
        return val
    return None


def _mbid_of(result: SearchResult) -> str | None:
    val = result.extras.get("mbid")
    if isinstance(val, str) and val:
        return val
    return None


def _popularity(result: SearchResult) -> float:
    val = result.extras.get("popularity")
    if isinstance(val, (int, float)):
        return float(val)
    return 0.0


def _completeness(result: SearchResult) -> int:
    """Count of non-None metadata fields — used for canonical selection."""
    count = 0
    if result.image_url:
        count += 1
    if result.extras.get("isrc"):
        count += 1
    if result.extras.get("duration_seconds") is not None:
        count += 1
    if result.extras.get("album"):
        count += 1
    return count


def _signature(result: SearchResult) -> str:
    """Normalized key for album-name stabilization."""
    title = normalize_for_match(result.title)
    subtitle = normalize_for_match(result.subtitle or "")
    return f"{subtitle}|{title}"


def _shares_word(result: SearchResult, query_norm: str) -> bool:
    """True if the result shares at least one word (len >= 2) with the query."""
    query_words = {w for w in query_norm.split() if len(w) >= 2}
    if not query_words:
        raw = query_norm.strip().split()
        return any(
            w in normalize_for_match(f"{result.subtitle or ''} {result.title}").split()
            for w in raw
            if w
        )
    text = normalize_for_match(f"{result.subtitle or ''} {result.title}")
    text_words = {w for w in text.split() if len(w) >= 2}
    return bool(query_words & text_words)


def _providers_of(result: SearchResult) -> set[ProviderName]:
    return {s.provider for s in result.sources}


def _merge(
    a: SearchResult,
    b: SearchResult,
    confidence: Confidence,
    tier: EntityResolutionTier,
) -> SearchResult:
    """Merge two results; more complete metadata wins canonical selection."""
    canonical, other = (a, b) if _completeness(a) >= _completeness(b) else (b, a)
    seen: set[tuple[ProviderName, str]] = set()
    sources: list[SourceRef] = []
    for s in (*canonical.sources, *other.sources):
        key = (s.provider, s.external_id)
        if key in seen:
            continue
        seen.add(key)
        sources.append(s)
    extras = {**other.extras}
    for k, v in canonical.extras.items():
        if v is not None or k not in extras:
            extras[k] = v
    pop = max(_popularity(a), _popularity(b))
    if pop > 0:
        extras["popularity"] = pop
    extras["resolution_tier"] = tier.value
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
    """Merge on shared identifiers first, then text similarity fallback.

    MBID mismatch blocks merge (different real-world entities). Otherwise
    ISRC > MBID > JW similarity (all kinds including artists).
    """
    if a.kind is not b.kind:
        return None
    isrc_a, isrc_b = _isrc_of(a), _isrc_of(b)
    if isrc_a and isrc_b and isrc_a == isrc_b:
        return _merge(a, b, Confidence.HIGH, EntityResolutionTier.ISRC)
    mbid_a, mbid_b = _mbid_of(a), _mbid_of(b)
    if mbid_a and mbid_b:
        if mbid_a == mbid_b:
            return _merge(a, b, Confidence.HIGH, EntityResolutionTier.MBID)
        return None
    sim = fuzz.token_sort_ratio(_signature(a), _signature(b)) / 100.0
    if sim >= 0.85:
        return _merge(
            a, b, Confidence.HIGH if sim >= 0.92 else Confidence.MEDIUM, EntityResolutionTier.NONE
        )
    return None


def _as_low_confidence(result: SearchResult) -> SearchResult:
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


def _content_words(text: str) -> set[str]:
    """Words >= 2 chars for content-token relevance scoring."""
    return {w for w in text.split() if len(w) >= 2}


def _relevance_score(result: SearchResult, query_norm: str) -> float:
    """Continuous query-relevance in [0, 1] via token_sort_ratio.

    Scores on both raw text and content words (>= 2 chars). The content-word
    path handles article mismatches: "The Weeknd" normalizes to "weeknd",
    so "blinding lights the weeknd" scores alike whether the article is
    present or stripped. Only ever RAISES the score (it's a max candidate).
    """
    query = query_norm.strip()
    if not query:
        return 0.0
    title = normalize_for_match(result.title)
    candidates = [fuzz.token_sort_ratio(query, title)]
    if result.subtitle:
        combined = f"{normalize_for_match(result.subtitle)} {title}".strip()
        candidates.append(fuzz.token_sort_ratio(query, combined))
    query_cw = _content_words(query)
    if query_cw:
        query_c = " ".join(sorted(query_cw))
        title_c = " ".join(sorted(_content_words(title)))
        candidates.append(fuzz.token_sort_ratio(query_c, title_c))
        if result.subtitle:
            combined_c = " ".join(sorted(_content_words(combined)))
            candidates.append(fuzz.token_sort_ratio(query_c, combined_c))
    return float(max(candidates)) / 100.0


@dataclass
class _Ranked:
    result: SearchResult
    best_rank: dict[ProviderName, int]


@dataclass
class _Scored:
    result: SearchResult
    relevance: float
    rrf: float


def fuse_and_rank(
    per_provider: Sequence[Sequence[SearchResult]],
    query_norm: str,
    *,
    quality_scorer: QualityScorer | None = None,
) -> tuple[SearchResult, ...]:
    """Merge on identifiers, rank by relevance.

    Merge: ISRC or MBID only. No text similarity.
    Gate: relevance > 0 (any non-zero fuzzy overlap).
    Sort: relevance-band → demotion → quality score → popularity → RRF → alpha.
    """
    # Pre-merge album-name stabilization
    _album_best: dict[str, tuple[str, int]] = {}
    for group in per_provider:
        for result in group:
            album = result.extras.get("album")
            if not isinstance(album, str) or not album:
                continue
            sig = _signature(result)
            comp = _completeness(result)
            prev = _album_best.get(sig)
            if prev is None or comp > prev[1]:
                _album_best[sig] = (album, comp)

    accumulated: list[_Ranked] = []
    for group in per_provider:
        for rank, incoming in enumerate(group):
            candidate = _as_low_confidence(incoming)
            cand_providers = _providers_of(candidate)
            for entry in accumulated:
                attempt = _try_merge(entry.result, candidate)
                if attempt is not None:
                    entry.result = attempt
                    for provider in cand_providers:
                        prev = entry.best_rank.get(provider)
                        entry.best_rank[provider] = rank if prev is None else min(prev, rank)
                    break
            else:
                accumulated.append(
                    _Ranked(result=candidate, best_rank=dict.fromkeys(cand_providers, rank))
                )

    for entry in accumulated:
        sig = _signature(entry.result)
        best = _album_best.get(sig)
        if best is not None and entry.result.extras.get("album") != best[0]:
            extras = {**entry.result.extras, "album": best[0]}
            entry.result = SearchResult(
                kind=entry.result.kind,
                title=entry.result.title,
                subtitle=entry.result.subtitle,
                image_url=entry.result.image_url,
                confidence=entry.result.confidence,
                sources=entry.result.sources,
                extras=extras,
            )

    scored: list[_Scored] = []
    for entry in accumulated:
        rrf = sum(1.0 / (_RRF_K + rank) for rank in entry.best_rank.values())
        result = entry.result
        if len(_providers_of(result)) < 2 and result.confidence is not Confidence.LOW:
            result = _as_low_confidence(result)
        extras = {**result.extras, "_rrf": rrf}
        result = SearchResult(
            kind=result.kind,
            title=result.title,
            subtitle=result.subtitle,
            image_url=result.image_url,
            confidence=result.confidence,
            sources=result.sources,
            extras=extras,
        )
        rel = _relevance_score(result, query_norm)
        if query_norm.strip() and not _shares_word(result, query_norm):
            continue
        scored.append(_Scored(result=result, relevance=rel, rrf=rrf))

    def _key(item: _Scored) -> tuple[float, int, int, float, float, float, str, str]:
        band = round(item.relevance, 1)
        demoted = 1 if is_demoted(item.result) else 0
        multi_source = 1 if len(_providers_of(item.result)) > 1 else 0
        popularity = _popularity(item.result)
        q_score = quality_scorer(item.result).composite if quality_scorer is not None else 0.0
        return (
            -band,
            demoted,
            -multi_source,
            -popularity,
            -q_score,
            -item.rrf,
            item.result.subtitle or "",
            item.result.title,
        )

    return tuple(item.result for item in sorted(scored, key=_key))


def rerank(results: Sequence[SearchResult], query_norm: str) -> tuple[SearchResult, ...]:
    """Re-sort after enrichment changed popularity. Same key as fuse_and_rank."""

    def _key(result: SearchResult) -> tuple[float, int, int, float, float, float, str, str]:
        band = round(_relevance_score(result, query_norm), 1)
        demoted = 1 if is_demoted(result) else 0
        multi_source = 1 if len(_providers_of(result)) > 1 else 0
        popularity = _popularity(result)
        rrf = float(result.extras.get("_rrf", 0.0))  # type: ignore[arg-type]
        return (
            -band,
            demoted,
            -multi_source,
            -popularity,
            -rrf,
            result.subtitle or "",
            result.title,
        )

    return tuple(sorted(results, key=_key))
