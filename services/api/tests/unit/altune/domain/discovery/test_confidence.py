"""Confidence enum ordering — slice 4 of discover-music-v1.

HIGH > MEDIUM > LOW, used by dedup ranking at slice 14.
"""

from __future__ import annotations

import pytest
from altune.domain.discovery.confidence import Confidence


@pytest.mark.unit
def test_confidence_orders_high_above_medium_above_low() -> None:
    assert Confidence.HIGH > Confidence.MEDIUM
    assert Confidence.MEDIUM > Confidence.LOW
    assert Confidence.HIGH > Confidence.LOW


@pytest.mark.unit
def test_confidence_has_three_members() -> None:
    assert {m.value for m in Confidence} == {"high", "medium", "low"}


@pytest.mark.unit
def test_confidence_round_trips_via_value() -> None:
    assert Confidence("high") is Confidence.HIGH
    assert Confidence("medium") is Confidence.MEDIUM
    assert Confidence("low") is Confidence.LOW
