"""ProviderStatus — per-provider response status in the scatter-gather output.

Per AC#5 (timeout), AC#5a (error), AC#5b (rate_limited), AC#6 (circuit_open).
OK is the happy path.
"""

from __future__ import annotations

from enum import Enum


class ProviderStatus(Enum):
    """Per-provider outcome of one scatter-gather call."""

    OK = "ok"
    TIMEOUT = "timeout"
    ERROR = "error"
    RATE_LIMITED = "rate_limited"
    CIRCUIT_OPEN = "circuit_open"
