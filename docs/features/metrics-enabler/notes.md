# Capability: go-api — metrics enabler

Epic #1193 (closed). Design: `docs/metrics-enabler.md` (what), `docs/metrics-enabler-design.md`
(how). This note is the assembled feature's memory — what now works, how to read it, and the
invariants it holds — confirmed on the whole feature at epic-close, not just per leaf. It unblocks
the Overseer Back-end performance bucket by making go-api performance readable at all.

## What now works

One endpoint, `GET /observe/metrics/live` (moved from `/admin/metrics/live` in #2805, gated to
`OVERSEER_PRINCIPAL_ID`), that returns the live in-process telemetry
as a single typed JSON response with two independently-built halves:

- **Module counters** — the per-module `expvar` degradation counters that already incremented but
  were never served: `auth` (`token_rejections_total` plus a bounded per-reason breakdown,
  `verifier_unavailable_total`, `jwks_fetch_failures_total`), `catalog`
  (`presign_failures_total`, `orphaned_deletes_total`, `stream_recoveries_total`,
  `db_call_timeouts_total`), and `feedback` (`tracker_create_failures_total`). Each module exposes
  a typed `ReadSnapshot()` over its own package-scope vars — the handler never touches the raw
  `expvar` registry, so process globals (`cmdline`, `memstats`) are never in the response.
- **Per-route latency histogram** (`latency`) — one router-level middleware
  (`internal/app/latency_middleware.go`, wired at `internal/app/routes.go:70`) times each request
  end-to-end and records the duration under the **matched chi route template** (e.g.
  `/v1/tracks/{trackId}`, never the raw path) into a bounded fixed-bucket histogram
  (`internal/shared/reqmetrics`). Buckets are fixed millisecond bounds
  `{1,5,10,25,50,100,250,500,1000,2500,5000}` plus a `+Inf` overflow, each a `RouteLatency`
  with `count`, `sum_ms`, and the cumulative bucket counts.

Deliberately deferred to a later slice (see design): true per-layer latency breakdown
(middleware/handler/service/adapter) — invasive, wide blast radius. v1 buys per-route latency for
near-zero cost.

## How to read it

- `GET /observe/metrics/live` with a bearer token. Behind the `/observe` group's
  `authMiddleware` + `observeHandler.Gate(OVERSEER_PRINCIPAL_ID)` (`internal/app/observe_wiring.go`).
  Unauthenticated → `401` (`WWW-Authenticate: Bearer`); authenticated non-principal → `403`
  (`observe.principal_required`). Both halves of the body share that one gate.
- Response shape: `{ "auth": {...}, "catalog": {...}, "feedback": {...}, "latency": { "routes":
  { "<template>": { "count", "sum_ms", "buckets": [{ "le_ms", "count" }, ...] } } } }`. Bucket
  labels are the `le_ms` upper bound as a string; the last is `"+Inf"`.
- Reserved latency keys: `unmatched` (requests that matched no chi route — 404s fold here) and
  `overflow` (routes past the cardinality cap fold here). Both always present.

## Invariants it keeps (confirmed at epic-close)

- **Gated, both halves.** Counters and latency are served only behind `observeHandler.Gate`
  (`gate_test.go`: `TestGate_AdmitsThePrincipal`, `TestGate_RefusesAnotherSubjectWithACodedError`,
  `TestGate_EmptyPrincipalAdmitsNobody`); the raw `expvar` `/debug/vars` handler is not mounted on
  the app's chi router. `reads_test.go` (`TestReadsMetricsLive_ServesTheSource`,
  `TestReadsMetricsLive_NilSourceAnswersEmptyObject`) covers the response body.
- **No per-request heap allocation on the hot path.** An already-seen route resolves through a
  `sync.Map` load and updates fixed-size atomic counters — no lock, no allocation. Asserted by
  benchmarks: `BenchmarkRecordLatency` and `BenchmarkRegistryObserve` both report **0 allocs/op**
  (`-benchmem`), plus `TestRecordLatency_ZeroAlloc` / `TestRegistryObserve_ZeroAllocHotPath`.
- **Recording failure never fails or slows a request.** The recorder runs after the response is
  served and under `defer recover()`; a panic in it is dropped and never reaches the client
  (`TestLatencyMiddleware_RecordPanicDoesNotFailRequest`).
- **Bounded route cardinality.** Route keys are chi templates (a finite static set), unmatched
  requests fold to one `unmatched` key, and distinct routes past `maxRoutes` (256) fold to one
  `overflow` key — a hostile client cannot grow the key set or memory without limit
  (`TestObserve_EmptyRouteFoldsToUnmatched`, `TestRegistry_BoundedCardinality`).
- **No PII in metrics.** Counters and latency only. Route keys are templates with placeholders
  (`{trackId}`), never real IDs; the auth per-reason map is keyed by a bounded `TokenRejectReason`
  enum (`missing`, `malformed`, `expired`, `signature_invalid`, ...), not attacker-controlled
  strings. No request content or user identifiers appear.

## Where it lives

- Endpoint + aggregation: `internal/observe/handler/metrics_live.go`
  (route registered in `internal/observe/handler/reads.go`, moved from
  `internal/admin/handler/metrics_live_handler.go` in #2805).
- Latency middleware: `internal/app/latency_middleware.go` (wired `internal/app/routes.go:70`).
- Histogram + snapshot: `internal/shared/reqmetrics/{histogram.go,snapshot.go}`.
- Module counters: `internal/{auth,catalog,feedback}/adapters/metrics/expvar_metrics.go`.
