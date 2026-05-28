# mypy: warn_unused_ignores = False, disable_error_code = "comparison-overlap,unreachable"
"""CircuitBreaker state machine — slice 25."""

from __future__ import annotations

import pytest

from altune.application.discovery.circuit_breaker import CircuitBreaker, CircuitState


class _FakeClock:
    def __init__(self) -> None:
        self.now: float = 0.0

    def __call__(self) -> float:
        return self.now

    def advance(self, seconds: float) -> None:
        self.now += seconds


@pytest.mark.unit
def test_breaker_starts_closed_and_allows_calls() -> None:
    cb = CircuitBreaker(name="deezer")
    assert cb.state is CircuitState.CLOSED
    assert cb.should_call() is True


@pytest.mark.unit
def test_breaker_opens_after_5_consecutive_failures() -> None:
    cb = CircuitBreaker(name="deezer")
    for _ in range(5):
        cb.record_failure()
    assert cb.state is CircuitState.OPEN
    assert cb.should_call() is False


@pytest.mark.unit
def test_breaker_resets_failure_count_on_success() -> None:
    cb = CircuitBreaker(name="deezer")
    cb.record_failure()
    cb.record_failure()
    cb.record_success()
    cb.record_failure()
    cb.record_failure()
    cb.record_failure()
    cb.record_failure()
    # Only 4 failures since the success; still closed.
    assert cb.state is CircuitState.CLOSED


@pytest.mark.unit
def test_breaker_half_opens_after_30s_via_injected_clock() -> None:
    clock = _FakeClock()
    cb = CircuitBreaker(name="deezer", clock=clock)
    for _ in range(5):
        cb.record_failure()
    assert cb.state is CircuitState.OPEN
    # 29s in → still open
    clock.advance(29.0)
    assert cb.state is CircuitState.OPEN
    # 30s in → half-open (allows one probe)
    clock.advance(1.5)
    assert cb.state is CircuitState.HALF_OPEN
    assert cb.should_call() is True


@pytest.mark.unit
def test_breaker_returns_to_closed_after_half_open_success() -> None:
    clock = _FakeClock()
    cb = CircuitBreaker(name="deezer", clock=clock)
    for _ in range(5):
        cb.record_failure()
    clock.advance(31.0)
    assert cb.state is CircuitState.HALF_OPEN
    cb.record_success()
    assert cb.state is CircuitState.CLOSED


@pytest.mark.unit
def test_breaker_reopens_when_half_open_probe_fails() -> None:
    clock = _FakeClock()
    cb = CircuitBreaker(name="deezer", clock=clock)
    for _ in range(5):
        cb.record_failure()
    clock.advance(31.0)
    assert cb.state is CircuitState.HALF_OPEN
    cb.record_failure()
    assert cb.state is CircuitState.OPEN
    # The new open window starts at the failure timestamp.
    clock.advance(29.0)
    assert cb.state is CircuitState.OPEN
    clock.advance(2.0)
    assert cb.state is CircuitState.HALF_OPEN


@pytest.mark.unit
def test_breaker_can_be_constructed_with_custom_threshold_and_window() -> None:
    cb = CircuitBreaker(name="quick", failure_threshold=2, open_duration_s=1.0)
    cb.record_failure()
    assert cb.state is CircuitState.CLOSED
    cb.record_failure()
    assert cb.state is CircuitState.OPEN
