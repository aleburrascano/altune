# Usage — design

Brief: `docs/usage.md`. Platform: `docs/overseer.md`, `docs/overseer-design.md`.

## Anchor & inherited invariants
- **Anchor:** what the owner does — plays, searches, sessions — over time, from bounded rollups.
- **Inherited spine:** bounded rollups · observe-only · owner-only · degrade-don't-crash, plus the
  platform spine.

## Grounded in
- Source: the operator SSE stream consumed by `services/overseer/internal/goapi/consumer.go`
  (`Events()`/`Status()`), same feed the Live activity bucket uses. Usage-relevant events: search
  events (`internal/discovery/service/telemetry.go`), playback events on the bus. The existing Live
  activity bucket at `services/overseer/internal/buckets/liveactivity/` is the pattern to copy.

## Significance
**Extends the Overseer plugin pattern** (a new bucket reusing the existing SSE consumer). One
decision: the rollup store shape. No new service; no go-api change in v1.

## Design decisions
- **Boundaries:** new bucket `services/overseer/internal/buckets/usage/`, consuming the **existing**
  `goapi.Consumer` (no new client methods → no overlap with the Reliability epic's `internal/goapi`
  additions; the two epics stay file-disjoint).
- **Data & state:** bounded rollups, not a raw ring — top-N searches, per-kind play counts, and a
  fixed-window activity timeline. Implement as a small **bounded aggregator** in the bucket
  (fixed top-N caps + a ring of time-bucketed counts). Reuse `core.RingStore` for the timeline;
  the top-N maps are capped.
  - *Over:* store raw events and aggregate on render — rejected, unbounded intake defeats
    bounded-storage; aggregate on ingest.
- **Source sharing:** both Usage and Live activity consume the same SSE stream. The SSE `Consumer`
  is single-consumer per connection; Usage opens its **own** consumer (its own connection) rather
  than sharing Live activity's — buckets stay independent, no cross-bucket coupling.
  - *Over:* fan out one consumer to both buckets — rejected for v1, it couples two buckets through
    shared state and breaks additive-buckets; revisit only if connection count becomes a concern.
- **Failure/degradation:** source down → serve last-known rollups flagged stale.

## Architectural invariants (add to epic spine)
- Usage aggregates on ingest; it never stores unbounded raw events.
- Usage opens its own SSE consumer; it shares no state with other buckets.

## Slice-1 build
1. Usage bucket skeleton consuming its own `goapi.Consumer`.
2. Bounded aggregator: top-N searches + play counts + windowed activity timeline.
3. Render panel; degrade-to-stale on source down. Plant: bounded-rollup test (N×input stays capped),
   stale-on-down test.

## Deferred (own later slice)
Historical backfill via a new go-api read endpoint over the Postgres event store.
