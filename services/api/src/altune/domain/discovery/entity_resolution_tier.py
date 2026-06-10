"""EntityResolutionTier — five-level enum for merge resolution identity confidence.

Replaces the merge-time role of Confidence. Drives merge decisions and feeds
into quality scoring. Ordered: MBID > ISRC > DURATION_CONFIRMED > FUZZY > NONE.
"""

from __future__ import annotations

from enum import Enum


class EntityResolutionTier(Enum):
    """Resolution tier used when merging two search results."""

    MBID = "mbid"
    ISRC = "isrc"
    NONE = "none"

    def _rank(self) -> int:
        return {
            EntityResolutionTier.MBID: 2,
            EntityResolutionTier.ISRC: 1,
            EntityResolutionTier.NONE: 0,
        }[self]

    def __gt__(self, other: object) -> bool:
        if not isinstance(other, EntityResolutionTier):
            return NotImplemented
        return self._rank() > other._rank()

    def __lt__(self, other: object) -> bool:
        if not isinstance(other, EntityResolutionTier):
            return NotImplemented
        return self._rank() < other._rank()

    def __ge__(self, other: object) -> bool:
        if not isinstance(other, EntityResolutionTier):
            return NotImplemented
        return self._rank() >= other._rank()

    def __le__(self, other: object) -> bool:
        if not isinstance(other, EntityResolutionTier):
            return NotImplemented
        return self._rank() <= other._rank()
