# Reliability — design

Brief: `docs/reliability.md`. Platform: `docs/overseer.md`, `docs/overseer-design.md`.

## Anchor & inherited invariants
- **Anchor:** at-a-glance "is the app up, what's degraded, what's alerting", from a view that
  survives the app going down.
- **Inherited spine:** independent down-detector · observe-only · bounded storage ·
  degrade-don't-crash · owner-only, plus the platform spine (plugin, no-internal-imports, etc.).

## Grounded in
- Reliability data sources: `internal/app/health.go` (dependency health), `internal/app/alerting.go`
  + `internal/admin/alert/monitor.go` (alert conditions/history), `/health` (open) and `/admin/health`
  (operator) routes. The Overseer's existing read client: `services/overseer/internal/goapi/client.go`
  (GET-only, `get` primitive, operator auth). SSE consumer + source-down typing already there.

## Significance
**Extends the Overseer plugin pattern** (a new bucket), with **one decision**: the independent
off-box reachability poll (new outbound behavior). No new store or service.

## Design decisions
- **Boundaries:** new bucket `services/overseer/internal/buckets/reliability/`. It reads go-api via
  the existing `goapi.Client`. New read methods (GET `/admin/health`, alerts) go in a **new file**
  `services/overseer/internal/goapi/health_reads.go`, NOT by editing `client.go` — keeps this epic's
  files disjoint from the Usage epic so they build in parallel without collision.
  - *Over:* editing `client.go` directly — rejected, it collides with any other epic touching the
    client in the same wave.
- **The independent poll:** a small poller in the bucket hits `/health` on a ticker (default 30s),
  recording up/down independent of the admin-API reads. This is the authoritative down-detector.
  - *Over:* deriving "up" from whether the admin read succeeded — rejected, that conflates "admin
    API degraded" with "app down" and can't distinguish them.
- **Data & state:** bounded `core.RingStore` for alert history + poll samples. No DB (v1).
- **Failure/degradation:** admin read unreachable → serve last-known mirrored health flagged stale;
  the own-poll signal stays live and authoritative.

## Architectural invariants (add to epic spine)
- Reliability's go-api reads live in their own file in `internal/goapi`, never editing shared
  `client.go`, so the bucket is additive at the file level.
- The reachability poll path shares no state with the admin-read path (independence is structural).

## Slice-1 build
1. Add `health_reads.go` to `internal/goapi` (GET `/admin/health` + alerts, read-only).
2. Reliability bucket: mirror panel (dep health pills + active alerts + recent history, bounded).
3. Independent reachability poller + its signal in the panel; degrade-to-stale on admin-read down.
   Plant: independent-down-detector test (app down → poll still reports down), bounded history.
