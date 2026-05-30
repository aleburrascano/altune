"""Unit tests for the AcquisitionStatus value object."""

from __future__ import annotations

from altune.domain.catalog.acquisition_status import AcquisitionStatus


def test_acquisition_status_pending_serializes_to_lowercase() -> None:
    assert AcquisitionStatus.PENDING.value == "pending"
