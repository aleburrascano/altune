# Usage (Overseer bucket)

Idea brief for shaping. Platform context: `docs/overseer.md`, `docs/overseer-design.md`. Bucket #7
on the menu, built on the existing plugin spine.

## Vision

What the owner actually does in the app — plays, searches, sessions — made visible over time.
Today usage events are *collected* (mobile posts to `/v1/discovery/events`, go-api persists them in
Postgres via `PgxEventStore.Append`, `internal/discovery/adapters/persistence/event_repo.go`), but
there is no view of them; from the owner's seat they are write-only.

## The idea

A Usage bucket (Collect/Store/Render plugin) that consumes the **live SSE event stream** (the same
source the Live activity bucket uses) and builds **bounded rollups**: top searches, play counts,
an activity-over-time view — rendered as a panel.

**Rejected alternatives:**
- *Query go-api's Postgres event store directly.* Rejected — violates the platform's
  observe-from-outside / owns-its-own-store invariants; the Overseer never touches go-api's DB.
- *Require a new go-api read endpoint first.* Rejected for v1 — couples the bucket to a go-api
  change and slows it; the live stream is enough to start.

## Assumptions

- The SSE stream carries the usage-relevant events (searches, plays). (Confirmed: search events via
  `Service.emitSearchEvent`; playback events flow through the event bus.)
- **[load-bearing]** Rollups built from the live stream are useful even though they only cover
  windows while the Overseer is running. Full history is a later enhancement.

## Scope / non-goals

**In:** bounded rollups from the SSE stream (top searches, play counts, activity timeline),
rendered as a panel.

**Out:**
- **Full historical backfill** from go-api's Postgres event store — needs a go-api read endpoint;
  deferred to a later slice.
- **Cross-device / multi-user** — single owner.
- **Any PII beyond the owner's own activity.**

## Priority

**Must:** live-stream rollups + panel.
**Then:** a go-api read endpoint for historical aggregates (its own later slice).

## Invariants

- **Bounded rollups:** fixed windows / top-N caps; no unbounded growth.
- **Observe-only** and **owner-only** (usage data is the owner's own).
- **Degrade-don't-crash:** serves last-known rollups flagged stale when the source is down.

## Open questions

Which event kinds count as "usage" — searches + plays confirmed; refine the exact set during build.
Historical backfill deferred (own slice).
