"""Scoring + outcome classification for the discovery eval harness.

The load-bearing distinction: a "miss" is either a RANKING_FAILURE (a provider
returned the expected entity but the ranker buried/dropped it — our bug) or a
COVERAGE_CEILING (no provider returned it — not our bug, a catalog gap). We
tell them apart by checking the raw per-provider results, not just the fused
output.

Matching reuses production normalization (`normalize_for_match`) so casing,
diacritics, punctuation and feature notation collapse the same way the real
ranker collapses them.
"""

from __future__ import annotations

from collections import defaultdict
from dataclasses import dataclass
from typing import TYPE_CHECKING

from altune.application.discovery.normalize import normalize_for_match

if TYPE_CHECKING:
    from collections.abc import Sequence

    from scripts.discovery_eval.corpus import EvalQuery

# Outcome classifications.
HIT_1 = "HIT@1"
HIT_3 = "HIT@3"
FOUND_LOW = "FOUND_LOW"
RANKING_FAILURE = "RANKING_FAILURE"
COVERAGE_CEILING = "COVERAGE_CEILING"


@dataclass(frozen=True, slots=True)
class SimpleResult:
    """A ranker/provider result reduced to what scoring needs."""

    kind: str
    title: str
    subtitle: str | None


@dataclass(frozen=True, slots=True)
class QueryOutcome:
    """The graded result of running one query."""

    query: str
    category: str
    source: str
    expected_kind: str
    expected_label: str
    classification: str
    rank: int | None
    top5: tuple[str, ...]


def _matches(result: SimpleResult, query: EvalQuery) -> bool:
    if result.kind != query.expected_kind:
        return False
    if normalize_for_match(result.title) != normalize_for_match(query.expected_title):
        return False
    if query.expected_subtitle is None:
        return True
    return normalize_for_match(result.subtitle or "") == normalize_for_match(
        query.expected_subtitle
    )


def _find_rank(results: Sequence[SimpleResult], query: EvalQuery) -> int | None:
    for i, r in enumerate(results):
        if _matches(r, query):
            return i
    return None


def classify(
    query: EvalQuery,
    ranked: Sequence[SimpleResult],
    *,
    raw: Sequence[SimpleResult],
) -> QueryOutcome:
    """Classify one query's outcome against the fused ranking + raw coverage."""
    rank = _find_rank(ranked, query)
    if rank == 0:
        classification = HIT_1
    elif rank is not None and rank < 3:
        classification = HIT_3
    elif rank is not None:
        classification = FOUND_LOW
    elif _find_rank(raw, query) is not None:
        classification = RANKING_FAILURE
    else:
        classification = COVERAGE_CEILING

    label = query.expected_title
    if query.expected_subtitle:
        label = f"{query.expected_title} — {query.expected_subtitle}"
    top5 = tuple(
        f"[{r.kind}] {r.title}" + (f" — {r.subtitle}" if r.subtitle else "") for r in ranked[:5]
    )
    return QueryOutcome(
        query=query.query,
        category=query.category,
        source=query.source,
        expected_kind=query.expected_kind,
        expected_label=label,
        classification=classification,
        rank=rank,
        top5=top5,
    )


def aggregate(outcomes: Sequence[QueryOutcome]) -> dict[str, dict[str, float]]:
    """Per-category (and `__all__`) metrics: n, hit@1, hit@3, mrr, coverage, ranking-fail rate."""
    groups: dict[str, list[QueryOutcome]] = defaultdict(list)
    for o in outcomes:
        groups[o.category].append(o)
        groups["__all__"].append(o)

    report: dict[str, dict[str, float]] = {}
    for category, items in groups.items():
        n = len(items)
        hit1 = sum(1 for o in items if o.rank == 0)
        hit3 = sum(1 for o in items if o.rank is not None and o.rank < 3)
        mrr = sum(1.0 / (o.rank + 1) for o in items if o.rank is not None)
        ranking_fail = sum(1 for o in items if o.classification == RANKING_FAILURE)
        covered = sum(1 for o in items if o.classification != COVERAGE_CEILING)
        report[category] = {
            "n": float(n),
            "hit@1": hit1 / n,
            "hit@3": hit3 / n,
            "mrr": mrr / n,
            "coverage": covered / n,
            "ranking_fail_rate": ranking_fail / n,
        }
    return report
