# Admin / Mission Control — router

Single-operator observability console under /admin. Deliberately NOT hexagonal — flat observability packages (`handler/`, `alert/`, `providerhealth/`, `requeststore/`, `ui/`) with consumer-defined interfaces implemented in the composition root.

Invariants:

- Fail-closed gating: shell/config are public by design (hold no data); every data route needs `auth.Middleware` + `OperatorOnly`, which denies all when `OPERATOR_USER_ID` is unset and re-checks auth itself.
- Privacy: alert messages and the event stream carry operational state names / type+timestamp only — never query text, user ids, hostnames, or connection strings.
- Memory bounds are load-bearing (4 GB box): requeststore 100 req / 64 KB body / 96 MB total; providerhealth 2048 samples/provider. Everything in-memory resets on restart.
- Discovery/acquisition never import admin — they feed it via consumer-defined seams. Don't invert that.
- SSE needs `http.Flusher` forwarded through every middleware wrapper.

Knowledge base: `okf/backend/admin.md` — read before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).
