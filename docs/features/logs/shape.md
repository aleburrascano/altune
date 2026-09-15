# Logs (Overseer bucket)

Idea brief. Platform: `docs/overseer.md`, `docs/overseer-design.md`. Built on the plugin spine. (Also a prerequisite for eventually retiring Mission Control, whose logs tab this replaces.)

## Vision
A live tail of what the app is logging, always available and filterable — so the owner can watch the app think without SSHing into a box.

## The idea
A Logs bucket that consumes go-api's log-ring SSE (`/admin/logs/stream`) into a **bounded live log panel**, filterable by level, degrading to stale on source down.

**Rejected alternatives:**
- Poll `/admin/logs` — rejected, the SSE stream is live; polling lags.
- Store all logs — rejected, a bounded ring only (the log ring is itself bounded upstream).

## Assumptions
- `/admin/logs/stream` emits structured log records carrying a level. (Confirm shape in design.)

## Scope / non-goals
**In:** bounded live log tail + filter by level, degrade-to-stale, rendered panel.
**Out:** log search over history / persistence (bounded ring only); log-based alerting.

## Priority
**Must:** live tail + level filter. **Then:** free-text filter.

## Invariants
- Bounded ring · observe-only · owner-only · degrade-don't-crash.
- **Render-escaping:** log text is watched-app data — HTML-escape it in the panel.

## Open questions
None blocking.
