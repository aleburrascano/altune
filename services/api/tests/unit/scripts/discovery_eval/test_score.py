"""Unit tests for the discovery-eval scorer / outcome classifier (pure)."""

from __future__ import annotations

import pytest
from scripts.discovery_eval.corpus import EvalQuery
from scripts.discovery_eval.score import (
    COVERAGE_CEILING,
    FOUND_LOW,
    HIT_1,
    HIT_3,
    RANKING_FAILURE,
    SimpleResult,
    aggregate,
    classify,
)

pytestmark = pytest.mark.unit


def _q(
    kind: str = "track",
    title: str = "Africa",
    subtitle: str | None = "Toto",
    category: str = "track_exact",
) -> EvalQuery:
    return EvalQuery("q", category, kind, title, subtitle, "library")


def _r(title: str, subtitle: str | None, kind: str = "track") -> SimpleResult:
    return SimpleResult(kind=kind, title=title, subtitle=subtitle)


def test_top_one_match_is_hit_at_1() -> None:
    ranked = [_r("Africa", "Toto"), _r("Africa", "Other")]
    outcome = classify(_q(), ranked, raw=ranked)
    assert outcome.classification == HIT_1
    assert outcome.rank == 0


def test_match_at_position_two_is_hit_at_3() -> None:
    ranked = [_r("Rosanna", "Toto"), _r("Hold the Line", "Toto"), _r("Africa", "Toto")]
    outcome = classify(_q(), ranked, raw=ranked)
    assert outcome.classification == HIT_3
    assert outcome.rank == 2


def test_match_below_top_three_is_found_low() -> None:
    ranked = [_r(f"Filler {i}", "X") for i in range(4)] + [_r("Africa", "Toto")]
    outcome = classify(_q(), ranked, raw=ranked)
    assert outcome.classification == FOUND_LOW
    assert outcome.rank == 4


def test_absent_from_ranked_but_in_raw_is_ranking_failure() -> None:
    ranked = [_r("Wrong", "Band")]
    raw = [_r("Africa", "Toto")]  # a provider returned it; the ranker dropped it
    outcome = classify(_q(), ranked, raw=raw)
    assert outcome.classification == RANKING_FAILURE
    assert outcome.rank is None


def test_absent_everywhere_is_coverage_ceiling() -> None:
    ranked = [_r("Wrong", "Band")]
    raw = [_r("Wrong", "Band")]
    outcome = classify(_q(), ranked, raw=raw)
    assert outcome.classification == COVERAGE_CEILING


def test_artist_query_matches_on_name_ignoring_subtitle() -> None:
    q = _q(kind="artist", title="Toto", subtitle=None, category="artist_only")
    ranked = [_r("Toto", None, kind="artist")]
    outcome = classify(q, ranked, raw=ranked)
    assert outcome.classification == HIT_1


def test_kind_mismatch_does_not_match() -> None:
    # An album titled "Africa" must not satisfy a track query for "Africa".
    q = _q(kind="track", title="Africa", subtitle="Toto")
    ranked = [_r("Africa", "Toto", kind="album")]
    outcome = classify(q, ranked, raw=ranked)
    assert outcome.classification == COVERAGE_CEILING


def test_normalization_tolerates_case_and_punctuation() -> None:
    q = _q(title="HUMBLE.", subtitle="Kendrick Lamar")
    ranked = [_r("humble", "kendrick lamar")]
    outcome = classify(q, ranked, raw=ranked)
    assert outcome.classification == HIT_1


def test_aggregate_computes_mrr_and_hit_rates_per_category() -> None:
    hit1 = classify(_q(category="track_exact"), [_r("Africa", "Toto")], raw=[_r("Africa", "Toto")])
    low = classify(
        _q(category="track_exact"),
        [_r("x", "y")] * 4 + [_r("Africa", "Toto")],
        raw=[_r("Africa", "Toto")],
    )
    agg = aggregate([hit1, low])
    cat = agg["track_exact"]
    assert cat["n"] == 2
    assert cat["hit@1"] == pytest.approx(0.5)
    assert cat["hit@3"] == pytest.approx(0.5)
    # MRR = mean(1/1, 1/5) = 0.6
    assert cat["mrr"] == pytest.approx(0.6)
