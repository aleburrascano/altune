---
date: 2026-06-19
topic: realtime-events
---

# Real-Time Events (SSE)

## Summary

Add an SSE endpoint to the Go API that pushes all domain events to connected mobile clients in real time, replacing the 30s polling loop as the primary state synchronization mechanism. An in-process event bus with per-user ring buffer persistence distributes events; the mobile app maps each received event to React Query cache invalidation.

---

## Problem Frame

Every server-side state change — acquisition completion, playlist mutation, track deletion — reaches the mobile client only when the client's next poll fires (30s `refetchInterval`) or the user manually pulls to refresh. The most painful gap is acquisition: the user saves a track, sees "pending," and gets zero feedback for up to 30s + acquisition time (30-120s) before the track shows as playable. The data-consistency contract (`docs/patterns/data-consistency.md`) defines a three-step cascade (fix → log → signal), but the "signal" step currently relies on HTTP responses and polling — there is no push channel.

Domain events already exist and are well-typed (7 catalog events, 2 discovery events). The missing piece is a transport to deliver them to connected clients.

---

## Actors

- A1. Mobile user: saves tracks, manages playlists, plays music — expects instant feedback on state changes
- A2. Go API: processes mutations, runs acquisition pipeline, emits domain events
- A3. Event bus: distributes events from producers (services) to consumers (SSE connections)

---

## Key Flows

- F1. Acquisition status push
  - **Trigger:** Acquisition pipeline completes or fails for a track
  - **Actors:** A2, A3, A1
  - **Steps:** Service marks track ready/failed → publishes event to bus → bus delivers to user's SSE connection → mobile receives event → invalidates library queries → UI updates instantly
  - **Outcome:** User sees track become playable within seconds of acquisition completing, not on next poll
  - **Covered by:** R1, R2, R5, R8

- F2. SSE connection lifecycle
  - **Trigger:** Mobile app enters foreground / user authenticates
  - **Actors:** A1, A2, A3
  - **Steps:** Client opens SSE connection with Bearer JWT → server authenticates → registers user channel → on disconnect (background/network), client reconnects with `Last-Event-ID` → server replays missed events from ring buffer → normal streaming resumes
  - **Outcome:** Client stays synchronized across foreground/background transitions without manual refresh
  - **Covered by:** R3, R4, R6, R7

---

## Requirements

**Event bus**
- R1. An in-process event bus distributes domain events to per-user subscribers using Go channels
- R2. All existing domain events are published: `TrackAddedToLibrary`, `TrackAcquisitionCompleted`, `TrackAcquisitionFailed`, `PlaylistCreated`, `PlaylistDeleted`, `TrackAddedToPlaylist`, `TrackRemovedFromPlaylist`, `SearchPerformed`, `ResultClicked`
- R3. Each event is assigned a monotonic per-user event ID for replay ordering
- R4. A per-user ring buffer retains the last N events (e.g., 100) so reconnecting clients can replay missed events via `Last-Event-ID`
- R5. The bus interface is a port (`EventPublisher` / `EventSubscriber`) so the in-process implementation can be swapped to Redis Pub/Sub later

**SSE endpoint**
- R6. `GET /v1/events` is an SSE endpoint authenticated via Bearer JWT (same as all other endpoints)
- R7. The endpoint sends events as JSON-encoded SSE `data` fields with `id` (monotonic) and `event` (domain event type name) fields
- R8. When `Last-Event-ID` header is present on reconnect, the endpoint replays all buffered events after that ID before switching to live streaming

**Mobile client**
- R9. The mobile app connects to SSE on foreground entry and disconnects on background
- R10. Each received event type maps to specific React Query `invalidateQueries` calls (e.g., `TrackAcquisitionCompleted` → invalidate `['library-home']`, `['library']`, `['playlist']`)
- R11. Polling remains as a fallback — SSE augments it, doesn't replace it. The `refetchInterval` can be relaxed (e.g., 30s → 120s) when SSE is connected
- R12. On reconnect, the client sends `Last-Event-ID` to replay missed events; if the server responds with HTTP 204 (buffer exhausted / ID too old), the client does a full refetch instead

---

## Acceptance Examples

- AE1. **Covers R1, R2, R8.** Given a user with an SSE connection open, when their track acquisition completes on the server, the mobile library screen updates to show the track as "ready" within 2 seconds — without the user pulling to refresh.
- AE2. **Covers R4, R7, R8, R12.** Given a user who backgrounds the app for 30 seconds during which 3 events fire, when they foreground the app and SSE reconnects with `Last-Event-ID`, the client receives all 3 missed events and invalidates the appropriate queries.
- AE3. **Covers R9, R11.** Given a user with poor connectivity where SSE disconnects, the polling fallback continues to operate at its normal interval, and the UI remains functional (degraded to polling, not broken).

---

## Success Criteria

- After saving a track, the user sees the status change to "ready" or "failed" within seconds of server-side completion, without manual refresh
- Playlist mutations by the same user reflect instantly across screens that show playlist data
- The SSE connection handles foreground/background transitions gracefully — no leaked connections, no missed events on short backgrounds (< ring buffer window)
- The polling fallback ensures the app never appears broken when SSE is unavailable

---

## Scope Boundaries

- Redis Pub/Sub event bus (deferred — port interface enables future swap)
- Supabase Realtime (rejected — bypasses API layer, couples mobile to DB schema)
- Push notifications for closed-app state (separate concern, separate spec)
- Multi-device sync optimization (SSE naturally enables it but requirements don't optimize for it)
- Bidirectional communication over the SSE channel (client→server stays HTTP)
- Event sourcing / full event log (ring buffer is ephemeral, not a persistent event store)

---

## Key Decisions

- **SSE over WebSocket:** Unidirectional server→client is all that's needed. SSE has built-in reconnection, works through proxies, and is simpler to implement. WebSocket's bidirectionality is unused overhead.
- **In-process bus over Redis:** Single Go instance, solo developer. The port interface makes Redis a future swap, not a current dependency.
- **Ring buffer for persistence:** Lightweight replay on reconnect without a database table. Buffer size (e.g., 100 events per user) covers typical background durations. Exhausted buffer triggers full refetch — acceptable for long disconnects.
- **SSE augments polling, doesn't replace it:** Polling at a relaxed interval is the safety net for connectivity issues. Removing polling entirely would create a single point of failure.

---

## Dependencies / Assumptions

- React Native SSE client library or polyfill needed (RN has no native `EventSource`)
- Bearer JWT auth works for long-lived SSE connections (token expiry handling needed — close connection on 401, mobile re-authenticates and reconnects)
- The existing domain event types in `services/go-api/internal/catalog/domain/events.go` and `services/go-api/internal/discovery/domain/events.go` are the complete set to publish

---

## Outstanding Questions

### Deferred to Planning

- [Affects R4][Technical] Optimal ring buffer size — 100 events per user is a starting point; may need tuning based on typical background duration and event frequency
- [Affects R10][Technical] Exact mapping of event types to React Query keys — needs verification against current query key structure
- [Affects R6][Technical] Token expiry during long SSE connections — should the server close the connection on token expiry, or validate only on initial connect?
