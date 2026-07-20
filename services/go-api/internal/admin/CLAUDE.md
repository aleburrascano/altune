# Admin / Mission Control — router

Single-operator observability console under /admin. Deliberately NOT hexagonal — flat observability packages (`handler/`, `alert/`, `evalmeter/`, `eventtap/`, `providerhealth/`, `requeststore/`, `ui/`). Interfaces exist only where the implementation lives in the composition root (`ReRunner`, `SearchInspector`, `AcquisitionStatusReader`, `HealthProbe`, `evalmeter.Runner`); same-feature collaborators are concrete pointers — don't reintroduce single-impl reader interfaces. `handler/` is transport only; background lifecycle components live in their own packages (`evalmeter.Meter`, `eventtap.Feed`). `eventtap.Tap` is the admin-owned `events.Publisher` decorator that feeds the console's system-wide event stream — its payload-key vocabulary stays here, not in `internal/shared/events`.

Invariants:

- Fail-closed gating: shell/config are public by design (hold no data); every data route needs `auth.Middleware` + `OperatorOnly`, which denies all when `OPERATOR_USER_ID` is unset and re-checks auth itself.
- Privacy: alert messages carry operational state names only — never query text, user ids, hostnames, or connection strings. The event stream is operator full-visibility (type + time + user + short subject) per the verbosity rework — acceptable only because the console is single-operator and auth-gated.
- Memory bounds are load-bearing (4 GB box): requeststore 100 req / 64 KB body / 96 MB total; providerhealth 2048 samples/provider. Everything in-memory resets on restart.
- Discovery/acquisition never import admin — they feed it via consumer-defined seams. Don't invert that.
- SSE needs `http.Flusher` forwarded through every middleware wrapper.

Knowledge base: `okf/backend/admin/index.md` — read before structural work; update the relevant concept file in the same commit when behavior it describes changes (pre-commit hook enforces).
