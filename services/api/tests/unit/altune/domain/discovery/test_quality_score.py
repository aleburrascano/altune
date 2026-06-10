"""Tests for QualityScore value object."""

from __future__ import annotations

import pytest

from altune.domain.discovery.quality_score import QualityScore


class TestQualityScore:
    def test_normalized_to_unit_interval(self) -> None:
        score = QualityScore(
            composite=0.75,
            completeness=0.8,
            agreement=0.6,
            entity_tier=0.9,
            fetch_success=0.7,
        )
        assert 0.0 <= score.composite <= 1.0

    def test_composite_below_zero_raises(self) -> None:
        with pytest.raises(ValueError, match="composite"):
            QualityScore(
                composite=-0.1,
                completeness=0.5,
                agreement=0.5,
                entity_tier=0.5,
                fetch_success=0.5,
            )

    def test_composite_above_one_raises(self) -> None:
        with pytest.raises(ValueError, match="composite"):
            QualityScore(
                composite=1.1,
                completeness=0.5,
                agreement=0.5,
                entity_tier=0.5,
                fetch_success=0.5,
            )

    def test_is_frozen(self) -> None:
        score = QualityScore(
            composite=0.5,
            completeness=0.5,
            agreement=0.5,
            entity_tier=0.5,
            fetch_success=0.5,
        )
        with pytest.raises(AttributeError):
            score.composite = 0.9  # type: ignore[misc]
