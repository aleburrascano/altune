`backoff` owns how long to wait before the next attempt of anything that retries: exponential from a base, bounded, with a jitter shape the caller names.

A `Policy` is built only by `MustCapped(base, cap, opts...)` or `MustUncapped(base, opts...)`, which panic at startup on a base or cap that is not positive, a cap below the base, a factor below 2 or a spread outside [0, 1]. `WithFactor(n)` sets the growth per attempt (2 when unset). `WithSpread(f)` picks symmetric jitter.

`Ceiling(attempt)` is the wait without jitter, for attempt 1, 2, 3 and so on; it grows by the factor and stops at the cap, or saturates when uncapped. `DelayAt(attempt, unit)` places the wait by `unit` in [0, 1), so a test passes the point it wants: without a spread it is equal jitter, within [ceiling/2, ceiling]; with a spread it is within ceiling plus or minus that fraction of the ceiling. `Delay(attempt)` draws `unit` at random. A caller whose tests pin exact waits uses `Ceiling`.
