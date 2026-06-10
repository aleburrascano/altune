"""ContentValidationStatus — per-(provider, external_id) fetch outcome."""

from __future__ import annotations

from enum import Enum


class ContentValidationStatus(Enum):
    """Cached content-fetch outcome for quality gate filtering."""

    FETCHABLE = "fetchable"
    UNFETCHABLE = "unfetchable"
    UNKNOWN = "unknown"
