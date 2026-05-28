"""Discovery domain events.

Past-tense + immutable. Emitted to logs (v1) and consumed by future
analytics persistence specs. Per ADR-0007 §"Domain events".
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime  # noqa: TC003

from altune.domain.discovery.confidence import Confidence  # noqa: TC001
from altune.domain.shared.user_id import UserId  # noqa: TC001


@dataclass(frozen=True, slots=True)
class SearchPerformed:
    """A discovery search was executed for a user."""

    query_norm: str
    user_id: UserId
    occurred_at: datetime
    total_results: int
    partial: bool


@dataclass(frozen=True, slots=True)
class ResultClicked:
    """A user clicked a result from a discovery response."""

    user_id: UserId
    query_norm: str
    result_signature: str
    position: int
    confidence: Confidence
    occurred_at: datetime
