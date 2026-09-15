# Logs — design

Brief: `docs/logs.md`. Platform: `docs/overseer-design.md`.

## Anchor & inherited invariants
- **Anchor:** a bounded live log tail, filterable by level, always available.
- **Inherited spine:** bounded ring · observe-only · owner-only · degrade-don't-crash · render-escaping.

## Grounded in
- Source (operator-only, confirmed): `GET /admin/logs/stream` (`streamLogs`, SSE over the in-process log ring) — `internal/admin/handler/admin_handler.go:80`. The Overseer already has SSE machinery: `internal/goapi/consumer.go` (reconnect/backoff/source-down) + `internal/goapi/sse.go` (frame decoder), currently pointed at `/admin/events/stream`.

## Significance
**Extends the plugin pattern**, with one wrinkle: a **second SSE stream**. The existing `Consumer` targets the events stream.

## Design decisions
- **Boundaries:** new bucket `services/overseer/internal/buckets/logs/`; a **logs SSE consumer** in a **new file** `internal/goapi/logs_consumer.go` that **reuses the existing SSE frame decoder** (`sse.go`) and reconnect/backoff/source-down typing, pointed at `/admin/logs/stream`, decoding structured log records (level + message + fields).
  - *Over:* generalize `Consumer` to take a path param and share one type — rejected for v1 to avoid touching the events consumer (keeps this epic's files disjoint); factor a shared core later if a third stream appears.
- **Data & state:** bounded `core.RingStore` of recent log records; render a tail filtered by level. HTML-escape every log field (watched-app data).
- **Degrade:** source down → last-known tail flagged stale.

## Architectural invariants (add to epic spine)
- The logs consumer reuses the SSE decoder without editing the events `Consumer`.
- Every rendered log field is HTML-escaped.

## Slice
**Single leaf** (logs_consumer.go + logs bucket + registration line). **Ready**. Shares only the `cmd/overseer/main.go` registration line with the other bucket epics (additive import).
