# Observe — Module Architecture

The observe bounded context (`services/go-api/internal/observe/`) is the operator-facing health, metrics, alert and live-tap surface of the backend.

- `domain/` — alert vocabulary: `Severity`, `Alert` and `Condition`.
- `ports/` — what the services need from outside: `AlertNotifier` and the `SilentNotifier` marker.
- `service/` — `alert` (the condition monitor), `evalmeter` (evaluation meter) and `eventtap` (live event tap and feed).
- `adapters/` — `notifier` delivers alerts: `WebhookNotifier` posts them, `NopNotifier` only logs.
