# Metrics enabler (go-api)

Idea brief for shaping. This is **go-api infrastructure**, not an Overseer bucket — the unlock that
makes the Back-end performance bucket possible. Platform context: `docs/overseer.md`,
`docs/overseer-design.md`.

## Vision

Make backend performance *measurable at all*. Today counters increment into `expvar` but nothing
serves them (`internal/catalog/adapters/metrics/expvar_metrics.go:1-4` — the scrape endpoint is
explicitly "out of scope"), and nothing times across layers. There is no OpenTelemetry/Prometheus.
So performance is currently invisible. This makes it readable, so the Overseer's Back-end
performance bucket (next round) has real data.

## The idea

Two additions to go-api:

1. **An operator-only metrics read endpoint** exposing the existing `expvar` counters in a shaped,
   authenticated form (under `/admin/*`, operator middleware).
2. **Basic per-layer latency timing** — capture time across middleware → handler → service →
   outbound adapters, exposed through the same endpoint.

**Rejected alternatives:**
- *Adopt full OpenTelemetry / Prometheus.* Rejected for now — heavy new deps and infra for a
  solo-operated app. The boring move (expose what already exists) delivers the first 80%; OTel can
  come if the data ever justifies it.
- *Wire the raw `expvar` `/debug/vars` handler.* Rejected — it leaks the whole process globals
  world-readable, is not operator-scoped, and is not shaped for the Overseer to consume.

## Assumptions

- The existing `expvar` counters (`catalog`, `feedback` adapters) are meaningful enough to expose.
- **[load-bearing]** Cross-layer latency can be captured with lightweight middleware/wrappers,
  without introducing a tracing framework.

## Scope / non-goals

**In:** an operator-only metrics read endpoint (counters), and basic per-layer latency capture +
exposure.

**Out:**
- **Full distributed tracing / OpenTelemetry.**
- **External scrapers / a Prometheus deployment.**
- **Dashboards / visualization** — that is the Overseer Back-end performance bucket's job; this
  enabler only makes the data readable.

## Priority

**Must:** expose the existing counters via an operator-only endpoint.
**Then:** per-layer latency timing.

## Invariants

- **Operator-only:** the metrics endpoint is never world-readable — `expvar`'s `/debug/vars` must
  not be exposed publicly.
- **Negligible hot-path overhead:** the timing instrumentation adds negligible latency to request
  handling (a bound a benchmark can check).
- **No PII in metrics** — counters and timings only, never request content or user identifiers.

## Open questions

Latency granularity — per-route vs per-layer. Start per-layer; refine during build.
