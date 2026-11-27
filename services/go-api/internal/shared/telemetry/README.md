`telemetry` owns how client and server telemetry events are recorded.

`RequestSpan(skip)` is the span observer for `httpserver.ObserveRequests`; the span carries the route template, status and correlation id of the shared outcome.

`Registry` is the one metrics home: `internal/app` creates one with `NewRegistry()` and passes it into each feature's metrics adapter (`auth.NewAuthMetrics`, `library.NewAudioStoreMetrics`, `playback.NewPlaybackMetrics`, `search.NewCountingTransport`), which take a `Counter`, `Gauge`, `LabeledCounter` or `Histogram` by name. A feature keeps no process-global counter, and `forbidigo` rejects `expvar.NewInt`, `NewMap` and the like outside this package's reach. The live metrics view reads the same registry through each feature's `ReadSnapshot(registry)`.

`StartStage(ctx, now, name, attrs...)` starts a child span of the request for provider calls and acquisition steps and returns the context to pass down. `Stage.End(err)` ends the span, marks it failed when `err` is not nil and returns the elapsed time on the injected clock, so the per-stage timings the search events and the download job log carry are derived from the same call.
