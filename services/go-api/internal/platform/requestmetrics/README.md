`requestmetrics` owns how request latency is measured and reported: the composition root creates one `Registry`, `Observer` records each request's route, duration and status into it as one of the observers of `httpserver.ObserveRequests`, and its `Snapshot` reports it.

## Must not

- Serve a route or tag a type for JSON: `Snapshot` has no json tags, and `monitoring` maps it to the `/observe/metrics/live` wire shape, reading it through the `monitoringfromrequestmetrics` bridge.
- Keep a global registry: `app` creates the one `Registry` and passes it in.

## Shape

The door is `requestmetrics.go`, with `New`. Under `internal/`: `service/` (the registry, its histogram bounds and the snapshot; the module has no business rules, so no `domain/`) and `adapters/observer` (the `httpserver` hook that records a finished request). `requestmetricstest/` holds `RouteLatency` for the alert tests in `app`.
