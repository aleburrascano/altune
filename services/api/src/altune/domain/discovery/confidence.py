"""Confidence — three-level dedup confidence enum.

Per ADR-0007 + discover-music-v1 spec §3.3: HIGH = ISRC-matched OR JW>=0.92,
MEDIUM = JW in [0.85, 0.92), LOW = standalone provider result.
"""

from __future__ import annotations

from enum import Enum


class Confidence(Enum):
    """Three-level dedup confidence; comparable HIGH > MEDIUM > LOW."""

    HIGH = "high"
    MEDIUM = "medium"
    LOW = "low"

    def _rank(self) -> int:
        return {Confidence.HIGH: 2, Confidence.MEDIUM: 1, Confidence.LOW: 0}[self]

    def __gt__(self, other: object) -> bool:
        if not isinstance(other, Confidence):
            return NotImplemented
        return self._rank() > other._rank()

    def __lt__(self, other: object) -> bool:
        if not isinstance(other, Confidence):
            return NotImplemented
        return self._rank() < other._rank()

    def __ge__(self, other: object) -> bool:
        if not isinstance(other, Confidence):
            return NotImplemented
        return self._rank() >= other._rank()

    def __le__(self, other: object) -> bool:
        if not isinstance(other, Confidence):
            return NotImplemented
        return self._rank() <= other._rank()
