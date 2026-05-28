"""Per-source CircuitBreaker — slice 25.

Three-state state machine: CLOSED -> OPEN (5 consecutive failures) -> HALF_OPEN
(after 30s) -> CLOSED (on success) or OPEN (on failure). Rate-limited
provider responses do NOT count as failures (per spec AC#5b).
"""

from __future__ import annotations

import logging
import time
from dataclasses import dataclass, field
from enum import Enum
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from collections.abc import Callable

_log = logging.getLogger(__name__)

_DEFAULT_FAILURE_THRESHOLD = 5
_DEFAULT_OPEN_DURATION_S = 30.0


class CircuitState(Enum):
    CLOSED = "closed"
    OPEN = "open"
    HALF_OPEN = "half_open"


@dataclass
class CircuitBreaker:
    """In-memory per-source circuit breaker.

    Not thread-safe; assumes single-event-loop access. clock() is injected
    so tests can advance time deterministically.
    """

    name: str
    failure_threshold: int = _DEFAULT_FAILURE_THRESHOLD
    open_duration_s: float = _DEFAULT_OPEN_DURATION_S
    clock: Callable[[], float] = field(default=time.monotonic)
    _state: CircuitState = field(default=CircuitState.CLOSED, init=False)
    _consecutive_failures: int = field(default=0, init=False)
    _opened_at: float | None = field(default=None, init=False)

    @property
    def state(self) -> CircuitState:
        """Public read; recomputes from CLOSED/HALF_OPEN/OPEN."""
        if self._state is CircuitState.OPEN and self._opened_at is not None:
            elapsed = self.clock() - self._opened_at
            if elapsed >= self.open_duration_s:
                self._transition(CircuitState.HALF_OPEN)
        return self._state

    def should_call(self) -> bool:
        """True iff a request should be attempted right now."""
        return self.state is not CircuitState.OPEN

    def record_success(self) -> None:
        self._consecutive_failures = 0
        if self._state is not CircuitState.CLOSED:
            self._transition(CircuitState.CLOSED)

    def record_failure(self) -> None:
        if self._state is CircuitState.HALF_OPEN:
            # A half-open probe failed → re-open with a fresh window.
            self._opened_at = self.clock()
            self._transition(CircuitState.OPEN)
            return
        self._consecutive_failures += 1
        if (
            self._state is CircuitState.CLOSED
            and self._consecutive_failures >= self.failure_threshold
        ):
            self._opened_at = self.clock()
            self._transition(CircuitState.OPEN)

    def _transition(self, new_state: CircuitState) -> None:
        if self._state is new_state:
            return
        _log.info(
            "circuit_breaker_state_change provider=%s old=%s new=%s failures=%d",
            self.name,
            self._state.value,
            new_state.value,
            self._consecutive_failures,
        )
        self._state = new_state
