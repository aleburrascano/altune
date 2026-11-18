# Observe — Module Architecture

The observe bounded context (`services/go-api/internal/observe/`) is the operator-facing health, metrics, alert and live-tap surface of the backend. The root package is the door.

- `domain/` — alert vocabulary: `Severity`, `Alert` and `Condition`.
- `internal/core/ports/` — what the services need from outside: `AlertNotifier` and the `SilentNotifier` marker.
- `internal/shell/` — `alert` (the condition monitor) and `eventtap` (live event tap and feed). `service/evalmeter` is the evaluation meter.
- `internal/adapters/` — `handler` serves the operator routes and streams behind the operator gate; `notifier` delivers alerts: `WebhookNotifier` posts them, `NopNotifier` only logs; `acquisitionbridge` and `discoverybridge` read those features' state; `codederr` holds the coded HTTP errors (status and `observe.*` code) the handler and `discoverybridge` return. The request user and token expiry come from `internal/shared/requestuser`.
