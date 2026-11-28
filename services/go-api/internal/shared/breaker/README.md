Infrastructure: `breaker` owns the circuit-breaker protocol that search's provider calls and playback's now-playing enrichment share.

One breaker guards one dependency; a caller that guards several keeps one `Breaker` per key.

`Allow(ctx)` returns an `Admission` and whether the call may go. Settle every admitted call exactly once: `Success`, `Failure`, or `Release` when the outcome says nothing about the dependency's health (the caller cancelled, the request was bad). `Failure` and `Success` take the admission so the breaker can tell a call admitted before a trip from one admitted after it.

The circuit opens when `Threshold` failures accumulate, or when a probe fails. It stays open for `OpenFor` after the last failure (a failure recorded while open restarts the window), then admits one probe. A probe holds the slot until it settles or `ProbeLease` passes, so an abandoned probe cannot wedge the circuit. Only a probe's success closes it. A success from a call admitted before the circuit tripped neither closes it nor resets the failure count of a later run, and a probe that lost its lease cannot free the slot of the probe that replaced it.

`Config.OnChange` is called under the breaker's lock for each transition (`Tripped`, `Probing`, `LeaseExpired`, `Recovered`), so a gauge driven from it always matches the state. It must be quick and must not call back into the breaker. A `Threshold` below 1 counts as 1; a nil `Now` is the wall clock. `OpenFor` and `ProbeLease` must be positive: a zero `OpenFor` admits a probe at once, and a zero `ProbeLease` lets every caller probe.
