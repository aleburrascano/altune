---
type: Subsystem
title: Admin request tracing
description: The correlation-id-keyed request-drill-down store — a bounded recording HTTP transport plus the discovery-fed search/detail trace, giving the operator "what came up when this request ran."
resource: services/go-api/internal/admin/requeststore/
tags: [admin, request-tracing, drill-down, transport, mission-control]
verified_commit: b1b3e3867ff5d3319beb9b3d361d8625cea3ec94
---

`requeststore.Store` (`store.go`) is the in-memory, correlation-id-keyed backing for the Mission Control request drill-down: every tracked discovery request accumulates the outbound provider exchanges it made plus — filled separately by the discovery handler — its query, user, per-provider trace, and final results. It resets on restart like every other Mission Control surface.

**Bounded on three axes**, load-bearing on the 4 GB production box: `defaultMaxRequests = 100` (oldest-first eviction of whole records via `dropOldest`), `defaultMaxBodyBytes = 64 * 1024` (per-response capture cap), `defaultMaxTotalByte = 96 * 1024 * 1024` (aggregate retained-body ceiling, enforced by `evictForBytes` dropping the oldest record until back under). `Record.bytes` tracks each record's own retained-body contribution so eviction can subtract it from the running `totalBytes` total in O(1).

**Two capture paths feed one record, keyed by correlation id:**

- `recordingTransport` (`transport.go`) wraps the shared live outbound transport *in the production search path*. `RoundTrip` reads the correlation id off the request context (`httputil.GetCorrelationID`); requests without one — or when no store is wired — pass straight through untouched, at zero cost. When present, the response body is wrapped in a `capturingBody` that tees up to `Store.MaxBodyBytes()` bytes into a buffer *as the adapter reads it* (no upfront full buffer) and finalizes the `Exchange` into the store on `Close` — the caller still receives the full, unmodified body.
- `RecordSearch`/`RecordContentFetch` (`trace.go`) are called at the discovery handler boundary, off the ranking path, to attach the query/user/per-provider-trace/final-results (search) or kind/provider/artist/items (`DetailTrace`, for artist discography/top-tracks/related detail-screen fetches) under the same correlation id the transport captured raw exchanges under. Both are no-ops without a correlation id. `ProjectStatuses`/`ProjectResults` are exported so the re-run inspector (see [console-surfaces](console-surfaces.md)) projects the identical display shape from a one-off recording instead of a live request. `ResultRow` carries the merge provenance needed to read *how* an entity was assembled — its contributing `Sources`, the `ResolutionTier` merge bound them by (isrc/mbid/bridge/title, from the `resolution_tier` extra), and the `Confidence` — and `ProviderTrace` carries an `Err` (the reason a lane returned nothing) so the drill-down and the re-run render a provider's failure, not just a zero count.

`ExchangeRecorder` (`exchange_recorder.go`) is a separate, simpler one-shot recorder for the operator-triggered re-run: unlike the corr-id-keyed transport it's scoped to a single request, so it can afford a full-body `io.ReadAll` (bodies still capped, recorder discarded after the response serializes) rather than the streaming-tee approach `recordingTransport` needs to stay cheap on every live search.

`Snapshot()`/`Get(corrID)` return deep copies (`cloneRecord`) so the operator drill-down can serialize freely without racing the store's writers; `Store` itself is a single `sync.Mutex` guarding both the exchange-recording write path and the read path.
