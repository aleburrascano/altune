"""Catalog domain events.

Past-tense + immutable, each carrying ``occurred_at``. Emitted to logs in v1
(future analytics specs may persist them). Mirrors `domain/discovery/events.py`.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime  # noqa: TC003

from altune.domain.catalog.track_id import TrackId  # noqa: TC001
from altune.domain.shared.user_id import UserId  # noqa: TC001


@dataclass(frozen=True, slots=True)
class TrackAddedToLibrary:
    """A track was added to a user's library (metadata only; audio pending)."""

    track_id: TrackId
    user_id: UserId
    occurred_at: datetime
