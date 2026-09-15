# Back-end performance (Overseer bucket)

Idea brief. Platform: `docs/overseer.md`, `docs/overseer-design.md`. Bucket #3. Built on the plugin spine. Depends on the metrics-enabler epic (#1193) landing.

## Vision
Backend latency is a feature for a music-streaming app. The owner should see, at a glance, how fast each route is (p50/p95/p99) and how much traffic it carries — so a slow path is obvious before users feel it. Today the data is only just becoming readable via the metrics enabler.

## The idea
A Back-end performance bucket (Collect/Store/Render plugin) that reads go-api's `GET /admin/metrics/live` (operator-only; the existing counters + the per-route latency histogram from #1198/#1199) and renders **per-route p50/p95/p99 latency + throughput**, with the **slowest routes highlighted**.

**Rejected alternatives:**
- Stand up Prometheus + scrape — rejected, the metrics enabler deliberately avoided that infra for a solo app.
- Compute percentiles from raw request samples in the Overseer — rejected, the endpoint already exposes a bounded histogram; estimate percentiles from it.

## Assumptions
- **[load-bearing]** `/admin/metrics/live` exposes a per-route latency histogram (fixed buckets) plus request counts, from which p50/p95/p99 can be estimated.

## Scope / non-goals
**In:** per-route p50/p95/p99 latency, throughput (request counts), slowest-routes highlight, rendered panel.
**Out:** per-layer (middleware/handler/service/adapter) breakdown (deferred in the metrics enabler itself); long-term historical trends; latency alerting.

## Priority
**Must:** per-route percentiles + throughput panel. **Then:** slowest-route highlight, short trend.

## Invariants
- Bounded storage (reads a snapshot; keeps only bounded recent samples).
- Observe-only · owner-only · degrade-don't-crash (stale on source down).

## Open questions
Percentile accuracy from fixed histogram buckets — acceptable for v1; note the bucket boundaries during design.
