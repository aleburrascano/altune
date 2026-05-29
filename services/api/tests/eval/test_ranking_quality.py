"""Ranking-quality regression guard for discovery search.

Drives the production ranker over the golden set and asserts MRR / top-3
thresholds. This is the executable form of the user-reported symptom:
"the right result shows up halfway down the list." A failure here means a
canonical match is being buried.
"""

from __future__ import annotations

import pytest

from altune.application.discovery.dedup import fuse_and_rank
from tests.eval.golden_cases import GOLDEN_CASES
from tests.eval.metrics import evaluate

# Target quality bar for the relevance-first ranker (RRF + exact-match boost).
_MIN_MRR = 0.85
_MIN_TOP3_HIT_RATE = 0.95


@pytest.mark.eval
def test_ranking_quality_meets_bar() -> None:
    report = evaluate(fuse_and_rank, GOLDEN_CASES)
    assert report.mrr >= _MIN_MRR, f"MRR below bar\n{report.format()}"
    assert report.top3_hit_rate >= _MIN_TOP3_HIT_RATE, (
        f"top-3 hit rate below bar\n{report.format()}"
    )
    # Unsatisfiable queries must not surface zero-relevance junk past the floor.
    assert report.junk_leaks == 0, f"junk leaked past the relevance floor\n{report.format()}"
