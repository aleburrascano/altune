`telemetry` owns how client and server telemetry events are recorded.

`RequestSpan(skip)` is the span observer for `httpserver.ObserveRequests`; the span carries the route template, status and correlation id of the shared outcome.
