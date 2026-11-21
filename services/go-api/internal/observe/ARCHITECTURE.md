# Observe — Module Architecture

The observe bounded context (`services/go-api/internal/observe/`) is the operator-facing health, metrics, alert and live-tap surface of the backend. The root package is the door.

- `internal/core/domain/` — alert vocabulary: `Severity` (named in words by `String`), `Alert` and `Condition`.
- `internal/core/ports/` — what observe needs from outside: `AlertNotifier` and the `SilentNotifier` marker, and the `AcquisitionReader` and `DiscographyQualityReader` the read routes serve, in observe's own types. `internal/app/observeacquisition` and `internal/app/observediscovery` implement the two readers from those features.
- `internal/shell/` — `alert` (the condition monitor, which stamps each firing's `Key` and `FiredAt` on the alert it pages and resends that same alert until it is delivered) and `eventtap` (live event tap and feed). `service/evalmeter` is the evaluation meter.
- `internal/adapters/` — `handler` serves the operator routes and streams behind the operator gate; `notifier` delivers alerts: `WebhookNotifier` posts them to a webhook, `EmailNotifier` sends each one as an email through Resend, with an `Idempotency-Key` from the alert's `Key` and `FiredAt` so a retried firing is delivered once (with the `onboarding@resend.dev` sender Resend only delivers to the Resend account owner's address, so any other `ALERT_EMAIL_TO` gets a 403 on every retry), `NopNotifier` only logs; `codederr` holds the coded HTTP errors (status and `observe.*` code) the handler returns. The request user and token expiry come from `internal/shared/requestuser`.
