`backoff` owns how long to wait before the next attempt of anything that retries outside the outbound HTTP transport: exponential from a base, capped, with equal jitter so the wait is never below half the ceiling.

`Ceiling(attempt)` is the wait without jitter, for attempt 1, 2, 3 and so on. `DelayAt(attempt, unit)` places the wait within [ceiling/2, ceiling] by `unit` in [0, 1), so a test passes the point it wants. `Delay(attempt)` draws `unit` at random.
