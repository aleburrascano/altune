"""Tests for ContentValidationStatus enum."""

from __future__ import annotations

import pytest

from altune.domain.discovery.content_validation_status import ContentValidationStatus


@pytest.mark.unit
def test_has_exactly_three_members() -> None:
    assert len(ContentValidationStatus) == 3


@pytest.mark.unit
def test_members_are_fetchable_unfetchable_unknown() -> None:
    assert ContentValidationStatus.FETCHABLE.value == "fetchable"
    assert ContentValidationStatus.UNFETCHABLE.value == "unfetchable"
    assert ContentValidationStatus.UNKNOWN.value == "unknown"
