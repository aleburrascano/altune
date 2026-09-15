# Back-end performance — design

Brief: `docs/backend-perf.md`. Platform: `docs/overseer-design.md`.

## Anchor & inherited invariants
- **Anchor:** per-route p50/p95/p99 latency + throughput, slowest routes surfaced.
- **Inherited spine:** bounded storage · observe-only · owner-only · degrade-don't-crash.

## Grounded in
- Source: `GET /admin/metrics/live` (operator-only), built by the metrics-enabler epic #1193 — counters (#1198) + per-route latency histogram (#1199). The Overseer read client is `services/overseer/internal/goapi/` (`get` primitive, operator auth).

## Significance
**Extends the plugin pattern** (a new bucket + one new goapi read file). No new infra.

## Design decisions
- **Boundaries:** new bucket `services/overseer/internal/buckets/backendperf/`; a new read method for `/admin/metrics/live` in a **new file** `internal/goapi/metrics_reads.go` (additive — do not edit `client.go`; register it on the observe-only allowlist).
- **Percentiles:** estimate p50/p95/p99 from the endpoint's fixed histogram buckets (linear interpolation within the bucket). *Over:* pull raw samples and compute exactly — rejected, the endpoint exposes a bounded histogram by design.
- **Data & state:** keep a bounded `core.RingStore` of recent snapshots for the throughput/trend; render current percentiles from the latest read.
- **Degrade:** source down → last-known flagged stale.

## Architectural invariants (add to epic spine)
- The metrics read lives in its own `internal/goapi` file, never editing `client.go`.

## Slice
**Single leaf** (metrics_reads.go + backendperf bucket + one registration line in `cmd/overseer/main.go`). **Blocked on epic #1193** — specifically #1199 (the latency fields) merged. Note: the registration line in `cmd/overseer/main.go` is the one file shared with the other new bucket epics — a pure additive import line (git normally auto-merges; crew rebases if it collides).
