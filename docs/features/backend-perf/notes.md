# Capability: Overseer — Back-end performance bucket

Epic #1346 (closed). Design: `docs/backend-perf.md` (what), `docs/backend-perf-design.md` (how).
Platform spine: `notes/overseer.md`. This note is the assembled bucket's memory — what now
works, how to run it, and the invariants it holds — confirmed on the whole bucket at epic-close,
not just per leaf.

## What now works

The Overseer's third bucket, `internal/buckets/backendperf`, answers "how fast is each go-api
route, and how much traffic does it carry" from a view that survives go-api going down. At a
glance it surfaces the slowest paths before users feel them.

- **Estimated percentiles** — reads go-api's `GET /observe/metrics/live` (moved from `/admin/metrics/live`
  in #2805, gated to `OVERSEER_PRINCIPAL_ID`; the per-route request-latency histogram built by the
  metrics-enabler epic #1193) via the read-only goapi client's `AdminMetricsLive()`, and estimates
  each route's **p50/p95/p99** by **linear
  interpolation within the fixed histogram buckets** (per-bucket, non-cumulative counts, inclusive
  upper bounds). No raw samples are pulled — the endpoint exposes none by design; percentiles are
  estimated from the bounded histogram. Accuracy is bounded by bucket width, acceptable for an
  operator view per the brief.
- **Overflow tail** — a percentile landing in the unbounded `+Inf` tail can only be bounded below,
  so it is flagged `Overflow` and rendered `≥Nms` (never read as exact). A route with zero observed
  requests is dropped — a percentile of nothing is meaningless.
- **Slowest-first highlight** — routes are sorted slowest-first by p99 (ties broken by throughput
  then name for a stable panel), the top three highlighted above the full per-route table, the #1
  slowest marked with a `slow` class.
- **Bounded throughput trend** — each collect records a total-requests-across-routes signal into a
  `core.RingStore` capped at 120, so memory is bounded no matter how long the service runs.
- **Degrade-to-stale** — when the metrics read is unreachable it serves the last-known latency
  flagged `STALE` rather than going dark; an unconfigured go-api (missing `OVERSEER_GOAPI_URL` /
  `_TOKEN`, or an invalid URL — the latter logged) degrades to a null reader that always reports
  source-down, never a startup crash.

- **go-api read** (`internal/goapi/metrics_reads.go`): `AdminMetricsLive()` plus the `LiveMetrics` /
  `LatencyMetrics` / `RouteLatency` / `LatencyBucket` shapes (a focused latency mirror; the
  endpoint's per-module counters are intentionally omitted and json-ignored, so go-api adding
  counters won't fail the decode). It is **additive at file level** — its own file, never editing
  `client.go` — and is registered on the observe-only allowlist (`observeonly_test.go`,
  `readOnlyMethods`), so the observe-only invariant cannot regress silently. Intake is bounded by
  the shared `get` primitive's 1 MiB `maxBodyBytes` cap, so a hostile/oversized histogram cannot
  exhaust memory.
- The bucket owns all its files and self-registers with one blank import (the additive-buckets
  invariant); the shell core references no concrete bucket.

## How to invoke it

- Config (env): reuses the platform's go-api source — `OVERSEER_GOAPI_URL`, `OVERSEER_GOAPI_TOKEN`
  (operator bearer). Missing/invalid config degrades to a null reader: the panel renders `STALE`
  rather than crashing the service (an invalid URL is logged, so a typo is not mistaken for a real
  outage).
- HTTP: the panel renders inside the owner-only shell (`GET /`). No new route. The source is
  go-api's `/observe/metrics/live`, gated to `OVERSEER_PRINCIPAL_ID`, reached with the bearer the
  client already attaches.
- Run locally: configure the go-api source, then run the Overseer as in `notes/overseer.md`
  (`cd services/overseer && OVERSEER_OWNER_TOKEN=<32+chars> go run ./cmd/overseer`).
- Gate (pinned toolchain — golangci crashes on Go 1.27): `GOTOOLCHAIN=go1.26.6`, then
  `go build ./... && go vet ./... && golangci-lint run --config ../go-api/.golangci.strict.yml ./...
  && CGO_ENABLED=1 go test -race ./... && govulncheck ./... && nilaway ./...`.

## Invariants it holds (spine + Back-end-performance-specific)

Confirmed on the assembled bucket at epic-close (green gate + a hostile attack pass):

- **percentile monotonicity** — p50 ≤ p95 ≤ p99 for every distribution: interpolation is bounded
  within `[lower, upper]` and buckets are walked in ascending order, so a higher quantile lands in
  the same or a later bucket at a higher position (proven empirically across 5000 random
  distributions at epic-close, plus the per-leaf interpolation/span/overflow/empty tests).
- **bounded intake** — the histogram read rides the goapi client's 1 MiB body cap; a hostile
  go-api sending an oversized histogram (many routes × many buckets) cannot exhaust memory, and the
  estimator handles it without algorithmic blow-up or panic.
- **observe-only** — the only new go-api method is a GET; it is on the client's read-only allowlist.
- **bounded storage** — the throughput trend is a single `RingStore` capped at 120 by construction
  (`TestStaysBoundedUnderLoad`).
- **degrade-don't-crash** — metrics read unreachable → last-known latency flagged `STALE`; an
  unconfigured source degrades to the null reader (source-down + stale panel), never a startup
  crash; the collect error still classifies as source-down for the shell.
- **owner-only** — the panel is reachable only through the owner-guarded shell.
- **render-escaping** — every watched-app field (route templates, throughput text) is HTML-escaped
  before entering `Panel.Body`; the LIVE/STALE line is a fixed literal, and estimated latencies are
  numeric-formatted. A hostile go-api route template cannot inject markup into the trusted panel
  (`TestRenderEscapesWatchedAppRoute`, plus an `"><img onerror>` attack at epic-close).
- **read/render race-free** — `Render` copies the snapshot struct under the lock and reads its map
  only after release; `recordFresh` only ever replaces `b.last` (never mutates in place), so the
  collect loop and HTTP render run concurrently without a data race (`go test -race` green, plus a
  stale-flip race probe at epic-close).
- **additive** — `metrics_reads.go` never edits `client.go`; core/shell/app depend on no concrete
  bucket; registration is one blank import.
- **no go-api internal imports** — the bucket reads go-api only over HTTP via the goapi client
  (module-wide guard `internal/guard/imports_test.go`).

## Hardened at epic-close (whole-bucket attack)

The attack on the assembled bucket found one defect the per-leaf gates couldn't see; fixed here:

1. **Malformed `NaN` bucket bound poisoned the estimate (LOW, hostile-source render)** —
   `bucketBound` treated only *parse-failing* labels as unbounded, but `strconv.ParseFloat` accepts
   `"NaN"`. A hostile/skewed go-api sending a `NaN` bucket label produced a `NaN` interpolated
   latency that rendered as `"NaNms"` in the panel — contradicting the function's own stated intent
   that a malformed bound degrades to unbounded. Fixed by also treating a non-finite (`NaN`) parsed
   bound as unbounded, so such a bucket degrades to the overflow tail (`≥…ms`) instead. Not a crash,
   XSS, or unbounded-memory issue — a cosmetic correctness fix on a hostile-source-only path.

Attacked and clean: percentile-estimation edges (empty histogram → zero, all-in-`+Inf` → overflow,
single finite bucket, monotonicity), oversized histogram (2000 routes × 500 buckets — no blow-up),
races between collect and render including stale-flip (`go test -race` green), XSS via crafted route
templates, degrade-to-stale on source-down and on unconfigured source, and shell survival on a
panicking bucket (`safeRender`).

## Not in this slice (roadmap)

Per-layer (middleware/handler/service/adapter) latency breakdown (deferred in the metrics enabler
itself), long-term historical latency trends, and latency alerting are all out of v1. Next buckets
per `notes/overseer.md`: Domain quality → Front-end health → Security → Cost.
