`requestmetrics` owns how request latency is measured and reported: the composition root creates one `Registry`, `Observer` records each request's route, duration and status into it as one of the observers of `httpserver.ObserveRequests`, and its `Snapshot` reports it.

## Must not

- Serve a route: `app/livemetrics` serializes `Snapshot`, so the json tags on it are the `/observe/metrics/live` wire shape.
- Keep a global registry: `app` creates the one `Registry` and passes it in.

## Shape

The door is `requestmetrics.go`. Under `internal/`: `domain/` (the registry, its histogram bounds and the tagged snapshot) and `adapters/observer` (the `httpserver` hook that records a finished request). `requestmetricstest/` holds `RouteLatency` for the alert tests in `app`.
