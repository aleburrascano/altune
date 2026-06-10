"""Signal-based quality scorer for search results.

Composite score from observable metadata signals. No keyword lists,
no similarity thresholds, no stopword sets. Demotion based on provider
record_type metadata only.
"""

from __future__ import annotations

from altune.domain.discovery.entity_resolution_tier import EntityResolutionTier
from altune.domain.discovery.quality_score import QualityScore
from altune.domain.discovery.search_result import SearchResult

_TIER_SCORES: dict[str, float] = {
    EntityResolutionTier.MBID.value: 1.0,
    EntityResolutionTier.ISRC.value: 0.8,
    EntityResolutionTier.NONE.value: 0.2,
}

_MAX_PROVIDERS = 6

_CANONICAL_RECORD_TYPES = frozenset({"album", "single", "ep"})


def _completeness(result: SearchResult) -> float:
    """Metadata completeness: has ISRC, image, duration, album -> [0, 1]."""
    fields = 0
    total = 4
    if result.extras.get("isrc"):
        fields += 1
    if result.image_url:
        fields += 1
    if result.extras.get("duration_seconds") is not None:
        fields += 1
    if result.extras.get("album"):
        fields += 1
    return fields / total


def _agreement(result: SearchResult) -> float:
    """Multi-source agreement: distinct providers / max providers -> [0, 1]."""
    providers = {s.provider for s in result.sources}
    return min(len(providers) / _MAX_PROVIDERS, 1.0)


def _entity_tier_signal(result: SearchResult) -> float:
    """Entity resolution tier -> [0, 1]."""
    tier_val = result.extras.get("resolution_tier")
    if isinstance(tier_val, str) and tier_val in _TIER_SCORES:
        return _TIER_SCORES[tier_val]
    return _TIER_SCORES[EntityResolutionTier.NONE.value]


def compute_quality_score(
    result: SearchResult,
    fetch_success_rate: float = 1.0,
) -> QualityScore:
    """Compute composite quality score from observable signals."""
    comp = _completeness(result)
    agr = _agreement(result)
    tier = _entity_tier_signal(result)
    fs = max(0.0, min(1.0, fetch_success_rate))
    composite = (comp + agr + tier + fs) / 4.0
    return QualityScore(
        composite=composite,
        completeness=comp,
        agreement=agr,
        entity_tier=tier,
        fetch_success=fs,
    )


def is_demoted(result: SearchResult) -> bool:
    """Demote results whose record_type is not in the canonical set.

    Canonical types: album, single, ep. Anything else (compilation, live,
    remix, demo, etc.) is demoted. No record_type = not demoted.
    """
    record_type = result.extras.get("record_type")
    if not isinstance(record_type, str):
        return False
    return record_type.lower() not in _CANONICAL_RECORD_TYPES
