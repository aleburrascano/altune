"""Track — aggregate root of the catalog bounded context.

A single audio recording with metadata (title, artist, optional album, optional
duration). Identity is a TrackId; the entity is owned by a user (UserId).

Equality is by identity (Track id) per domain-layer.md's entity rule, not by
attribute. Two Track instances with the same TrackId are the same Track from
the domain's perspective, even if their other attributes differ.

Invariants enforced in __post_init__:
- title is non-empty
- artist is non-empty
- duration_seconds, when present, is non-negative

STUB: __post_init__ validation and __eq__/__hash__ overrides land in the GREEN
commit. This file currently provides the field shape only.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime  # noqa: TC003  # used as runtime annotation by dataclass

from altune.domain.catalog.track_id import TrackId  # noqa: TC001
from altune.domain.shared.user_id import UserId  # noqa: TC001


@dataclass(frozen=True, slots=True)
class Track:
    id: TrackId
    user_id: UserId
    title: str
    artist: str
    album: str | None
    duration_seconds: int | None
    added_at: datetime
