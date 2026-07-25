# Admin / Mission Control — router

Single-operator observability console under `/admin`. Deliberately **not** hexagonal — flat observability packages.

Layout:

- `handler/` — transport only; `AdminHandler`, `OperatorOnly`, the per-panel endpoints, `sse.go`.
- `alert/` — `Monitor`, `Condition`, `NopNotifier` / `NtfyNotifier`.
- `evalmeter/` — `Meter`, the background eval ticker.
- `eventtap/` — `Tap` (the `events.Publisher` decorator) and `Feed`.
- `providerhealth/` — rolling per-provider outcome window.
- `requeststore/` — correlation-keyed drill-down store, recording transport, trace projections.
- `ui/` — the embedded console page.

## Rules

- Every data route needs `auth.Middleware` + `OperatorOnly`; only the shell and `/admin/config` are public.
- `OperatorOnly` stays fail-closed — unset `OPERATOR_USER_ID` denies everyone — and re-checks auth itself.
- Never put query text, user ids, hostnames or connection strings in an alert message.
- Only `SeveritySignal` may page; lower tiers log.
- Never give the eval meter production's circuit breakers, and never let two runs overlap.
- Never add a second `Tap` consumer, and never block `Publish` on a slow one.
- Keep the memory bounds — 100 requests / 64 KB body / 96 MB total, 2048 samples per provider.
- Never let discovery or acquisition import admin; they feed it through consumer-defined seams.
- Keep `handler/` transport-only — background lifecycle components live in their own package.
- Keep the tap's payload vocabulary here, never in `internal/shared/events`.
- Hold same-feature collaborators as concrete pointers; don't reintroduce single-impl reader interfaces.
- Forward `http.Flusher` through every middleware wrapper or SSE breaks.

Why each rule exists: `okf/backend/admin/index.md` — read before structural work; update the relevant concept in the same commit when behavior it describes changes (pre-commit hook enforces).
