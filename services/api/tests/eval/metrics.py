"""Ranking-quality metrics for the discovery eval harness.

Pure functions over a ranking callable and the golden dataset. A ranking
callable has the post-overhaul signature ``(per_provider_groups, query_norm)
-> ranked tuple`` so the same harness drives the legacy ranker (via an
adapter that flattens the groups) and the new RRF ranker unchanged.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import TYPE_CHECKING, Protocol

from altune.application.discovery.normalize import normalize_for_match

if TYPE_CHECKING:
    from collections.abc import Sequence

    from altune.domain.discovery.search_result import SearchResult
    from tests.eval.golden_cases import GoldenCase


class RankFn(Protocol):
    """Ranking callable under evaluation."""

    def __call__(
        self,
        per_provider: Sequence[Sequence[SearchResult]],
        query_norm: str,
    ) -> tuple[SearchResult, ...]: ...


@dataclass(frozen=True)
class EvalReport:
    """Aggregate ranking-quality metrics over the golden set.

    MRR / top-3 are computed over the *satisfiable* cases (those with a known
    canonical). `junk_leaks` counts `forbidden` entries that survived the
    relevance floor across *unsatisfiable* cases — it must be 0.
    """

    mrr: float
    top3_hit_rate: float
    junk_leaks: int
    per_case: dict[str, str]  # query -> human-readable outcome

    def format(self) -> str:
        lines = [
            f"MRR={self.mrr:.3f}  top3_hit_rate={self.top3_hit_rate:.3f}  "
            f"junk_leaks={self.junk_leaks}",
            "per-case:",
        ]
        for query, outcome in self.per_case.items():
            lines.append(f"  {outcome:>10}  {query}")
        return "\n".join(lines)


def _matches_pair(result: SearchResult, title: str, subtitle: str | None) -> bool:
    """Match on normalized (title, subtitle) so the canonical representative
    chosen by the ranker (which may differ in raw text) still counts."""
    return normalize_for_match(result.title) == normalize_for_match(title) and (
        normalize_for_match(result.subtitle or "") == normalize_for_match(subtitle or "")
    )


def expected_rank(ranked: tuple[SearchResult, ...], case: GoldenCase) -> int | None:
    """Zero-based position of the expected answer, or None if absent/unsatisfiable."""
    if case.expected_title is None:
        return None
    for i, result in enumerate(ranked):
        if _matches_pair(result, case.expected_title, case.expected_subtitle):
            return i
    return None


def evaluate(rank_fn: RankFn, cases: Sequence[GoldenCase]) -> EvalReport:
    """Run the ranker over every case.

    MRR + top-3 over satisfiable cases (known canonical); junk_leaks over
    unsatisfiable cases (forbidden entries that survived the relevance floor).
    """
    reciprocal_sum = 0.0
    top3_hits = 0
    satisfiable = 0
    junk_leaks = 0
    per_case: dict[str, str] = {}
    for case in cases:
        ranked = rank_fn(case.provider_groups(), normalize_for_match(case.query))
        if case.expected_title is None:
            leaks = [
                f"{t}/{s}"
                for (t, s) in case.forbidden
                if any(_matches_pair(r, t, s) for r in ranked)
            ]
            junk_leaks += len(leaks)
            per_case[case.query] = "JUNK!" if leaks else "no-junk"
            continue
        satisfiable += 1
        rank = expected_rank(ranked, case)
        if rank is None:
            per_case[case.query] = "MISSING"
        else:
            per_case[case.query] = f"#{rank}"
            reciprocal_sum += 1.0 / (rank + 1)
            if rank < 3:
                top3_hits += 1
    return EvalReport(
        mrr=reciprocal_sum / satisfiable if satisfiable else 0.0,
        top3_hit_rate=top3_hits / satisfiable if satisfiable else 0.0,
        junk_leaks=junk_leaks,
        per_case=per_case,
    )
