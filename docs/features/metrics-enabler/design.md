# Metrics enabler — design

Brief: `docs/metrics-enabler.md`. This is go-api infrastructure, not an Overseer bucket.

## Anchor & inherited invariants
- **Anchor:** make backend performance measurable — expose the counters that exist but aren't
  served, and start timing requests, so the Overseer's perf bucket has real data.
- **Inherited spine:** operator-only · negligible hot-path overhead · no PII in metrics.

## Grounded in
- Counters exist but unserved: `internal/catalog/adapters/metrics/expvar_metrics.go:1-4`,
  `internal/feedback/adapters/metrics/expvar_metrics.go`. No `/debug/vars` wired anywhere.
- Operator surface + middleware chain: `internal/app/routes.go:26-41` (correlation/recover/logger),
  `/admin/*` behind `OperatorOnly` (`internal/app/admin_wiring.go:59-67`), admin routes in
  `internal/admin/handler/admin_handler.go:77-92`.
- **Name clash to avoid:** `/admin/metrics` already exists — the 30-day rollup history
  (`internal/admin/handler/metrics_handler.go:11-40`). The new counters/latency go on a DIFFERENT
  sub-route.

## Significance
**Significant** — a new operator endpoint plus request-timing instrumentation on the hot path.
Walk the lenses.

## Design decisions (lens by lens)
- **Boundaries:** add an operator-only read route (e.g. `GET /admin/metrics/live`) that aggregates
  the per-module `expvar` counters into one JSON response. Reuse the admin handler + `OperatorOnly`.
  - *Over:* wiring raw `expvar` `/debug/vars` — rejected, world-readable, unscoped, leaks all
    process globals. *Over:* reusing `/admin/metrics` — rejected, clashes with the rollup history.
- **Scaling / hot paths (the crux):** request latency is captured by **one middleware** at the
  router (total per-route duration) into a **bounded fixed-bucket histogram** using atomic adds and
  **no per-request heap allocation**. That is the whole hot-path cost — one cheap stamp per request.
  - *Over:* full per-layer spans (middleware→handler→service→adapter) — rejected for v1: it means
    instrumenting many files across every module (huge blast radius) and per-request allocation.
    Deferred to its own later slice. v1 buys per-route latency for near-zero cost and small blast.
- **Data & state:** counters are the existing in-process `expvar`; latency is a new bounded
  in-process histogram (fixed buckets, capped). No DB, no new store type.
- **Failure/degradation:** metrics recording is best-effort — a panic in the recording path is
  recovered and dropped; it must never affect request handling.
- **Infra/tech-stack fit:** stdlib `expvar` + a tiny hand-rolled histogram. **No new deps** — keeps
  the deliberate "no OpenTelemetry/Prometheus yet" stance from the brief.

## Architectural invariants (add to epic spine)
- The metrics read endpoint is operator-only; `expvar`'s `/debug/vars` is never exposed publicly.
- Request-timing adds **no per-request heap allocation** on the hot path (a benchmark asserts it).
- A failure in metrics recording never fails or slows a request (recovered, dropped).

## Slice-1 build
1. Operator-only `GET /admin/metrics/live` aggregating existing `expvar` counters (catalog +
   feedback) into one JSON response.
2. One router-level latency middleware → bounded per-route histogram, exposed on the same endpoint.
   Plant: operator-only test (unauth/non-operator rejected), no-alloc benchmark on the hot path,
   recording-panic-is-contained test.

## Deferred (own later slice)
True per-layer latency breakdown (handler/service/adapter instrumentation) — invasive, wide blast
radius; do it when the perf bucket proves per-route isn't enough.
