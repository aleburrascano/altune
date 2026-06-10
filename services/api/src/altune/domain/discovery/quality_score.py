"""QualityScore — composite signal score for search result quality.

Value object carrying a normalized [0, 1] composite score and per-signal
breakdowns. Replaces static provider priors as the ranking signal.
"""

from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True)
class QualityScore:
    """Composite quality score for a search result."""

    composite: float
    completeness: float
    agreement: float
    entity_tier: float
    fetch_success: float

    def __post_init__(self) -> None:
        if not 0.0 <= self.composite <= 1.0:
            msg = f"composite must be in [0, 1], got {self.composite}"
            raise ValueError(msg)
