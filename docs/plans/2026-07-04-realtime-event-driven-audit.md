# Realtime / Event-Driven Architecture Audit & Remediation Plan

**Date:** 2026-07-04
**Status:** Audit complete — remediation not started
**Goal:** The mobile client should never need to poll or pull-to-refresh. Every server-side state change is pushed over SSE and patched into the client cache instantly.

> Pick-up instructions for a new session: read the **TL;DR** and **How to resume** sections first, then jump to the wave you're implementing. Every finding has a `file:line` anchor. Findings are labelled `F1`–`F17`; reference them by id in commits/PRs.

---

## TL;DR

The app already has a full SSE realtime system (backend in-process event bus → `/v1/events` SSE handler → client `sse-client` → `applyServerEvent` → React Query cache patches + a per-track stage store). It is **not missing** — it is **leaky**, and one bug makes it barely hold a live connection.

- **The root cause of the "why do I have to pull-to-refresh" feeling is `F1`:** the SSE handler returns HTTP `204` and closes on any caught-up reconnect or after a server restart, which prevents a live stream from staying open. Fix this first — it's small and removes the felt problem.
- **The download-progress flow you actually hit** breaks because list membership comes from the cached `acquisition_status === 'pending'` field while the phase caption comes from the SSE stage store — two sources of truth — and the `completed` event drops the item and clears the stage in the same tick, so "Finishing up…" never paints and the row vanishes (`F6`, `F7`, `F8`).
- Everything else is: **patch instead of invalidate/poll** (payloads too thin, `F10`–`F12`), **missing playlist events** (`F13`), and **removing the polling/staleness crutches** once realtime is trustworthy (`F14`–`F17`).

**Recommended order:** Wave 1 (SSE reliability) → Wave 2 (download flow) → Wave 3 (payloads/patch) → Wave 4 (playlist events) → Wave 5 (remove crutches, last).

---

## Background / how we got here

This audit was spun up at the tail of a production incident (recently-downloaded tracks were unplayable — root cause was the ID3v2 tagger corrupting m4a files; fixed by reverting the acquisition pipeline to MP3 + adding a decode gate; see commits `5b1d31f`, `8c8f05d`, `263469c`, `f9706d6`). While verifying the fix by re-acquiring a track, the user observed the acquisition progress UI was not realtime: it showed the first phase then jumped to done, and sometimes required a pull-to-refresh to reflect state. That observation prompted this codebase-wide event-driven-architecture sweep.

Three parallel read-only audits were run:
1. Backend event-emission coverage + SSE handler/bus reliability.
2. Frontend SSE consumption + reliability + full polling/pull-to-refresh inventory.
3. The acquisition/download progress chain end-to-end.

Their findings are consolidated below (nothing dropped; per-audit raw detail is preserved in the **Appendix**).

---

## Current architecture map

**Backend (Go)**
- In-process event bus: `services/go-api/internal/shared/events/bus.go` (`InProcessBus`). Per-user ring buffer (`defaultRingSize = 100`, `bus.go:14`), per-subscriber channel (`subscriberChanSize = 16`, `bus.go:15`). Monotonic `nextID` per process, **resets to 0 on restart** (`bus.go:82`).
- SSE handler: `services/go-api/internal/app/sse_handler.go`, mounted at `r.Handle("/events", ...)` (`app.go:377`). Reads `Last-Event-ID` (`sse_handler.go:40-53`), replays via `bus.Replay` (`bus.go:188-229`), then `Subscribe` (`sse_handler.go:55`). Sets `X-Accel-Buffering: no` (`:38`).
- Producers publish via `events.Publish(userId, type, payload)`. **8 event types total** (full table in Appendix A).

**Frontend (React Native / Expo)**
- SSE client (RN has no native `EventSource`): `apps/mobile/src/shared/events/sse-client.ts` — XMLHttpRequest streaming, `Last-Event-ID` on reconnect (`:64`), fixed 3s reconnect (`:119-125`).
- Connection lifecycle: `apps/mobile/src/shared/events/useServerEvents.ts` — mounted at root `apps/mobile/src/app/_layout.tsx:41`. Connects on foreground+auth, disconnects on background, reconnects on foreground/loss.
- Event router: `apps/mobile/src/shared/events/applyServerEvent.ts` — maps events to React Query cache patches (`patchTrackInCaches` in `trackCachePatch.ts`) or `invalidateQueries`, plus the acquisition stage store.
- Acquisition stage: `apps/mobile/src/shared/acquisition/stageStore.ts` (ephemeral per-track stage, Zustand), `stagePhase.ts` (6 stages → 3 phases), `activeDownloads.ts` (`deriveActiveDownloads` — derives in-flight list from cache `pending`), `useActiveDownloads.ts`.
- Display: `features/playback/ui/{ActivityDock,DownloadsBar,DownloadsSheet}.tsx`, `features/library/ui/LibraryRow.tsx`.
- Server state: React Query, global `staleTime: 30_000` (`_layout.tsx:55`).

---

## Consolidated findings (F1–F17), grouped into fix-waves

Severity: 🔴 Critical · 🟠 High · 🟡 Medium · 🟢 Low. Do the waves in order — later waves assume earlier ones landed.

### Wave 1 — SSE reliability foundation (nothing is trustworthy until the stream stays open)

**F1 · 🔴 Critical — Caught-up/after-restart reconnect returns `204` and closes the stream.**
`services/go-api/internal/app/sse_handler.go:40-53`. On reconnect the client sends `Last-Event-ID`; server calls `bus.Replay(userId, id)` which returns `nil` when there are **no events newer than the cursor** (`bus.go:220-228`) — the *normal* caught-up reconnect — and also after a server restart (user not in map → `bus.go:190-192` returns nil). The handler then does `w.WriteHeader(204); return` (`sse_handler.go:44-46`), **never reaching `Subscribe` (`:55`)**. Per the EventSource contract `204` means "stop", and the XHR client instead hot-loops: `onloadend` → `scheduleReconnect` (`sse-client.ts:82-86`) → 3s later reconnect with the same id → 204 again. **Effect:** while idle+caught-up (every foreground, every blip) no live stream is held open; the next real event is up to 3s late (via the replay branch once non-nil), plus continuous battery/network churn. This is the primary reason the UI feels dead and users pull-to-refresh.
**Fix:** on empty replay, do **not** `204` — fall through to `Subscribe` and stream heartbeats/live events. Reserve non-2xx for auth failure only.

**F2 · 🟠 High — No heartbeat/keepalive → silent dead connections.**
`services/go-api/internal/app/sse_handler.go:61-70` (stream loop only writes on real events; confirmed no `time.Ticker`/ping). A proxy/carrier idle-timeout can drop the socket without the client's `onerror`/`onloadend` firing (XHR `onprogress` just stops). No client-side idle watchdog exists either. Silently-dead connection = missed events = stale UI with no reconnect trigger. `X-Accel-Buffering: no` (`:38`) does not substitute for a heartbeat.
**Fix:** server emits a comment ping (`:\n\n`) every ~20–30s via a `time.Ticker` in the select; client resets a watchdog on each `onprogress` and force-reconnects if it elapses.

**F3 · 🟡 Medium — XHR `responseText` grows unbounded on a long-lived stream.**
`apps/mobile/src/shared/events/sse-client.ts:70-71`. `processedLength` substrings out new text, but `xhr.responseText` retains the **entire** stream for the connection's lifetime. Masked today only because F1 keeps killing connections; **fixing F1 exposes this as a real memory leak** on hours-long streams.
**Fix:** periodically recycle the XHR (close + reconnect with `Last-Event-ID`), or cap connection lifetime.

**F4 · 🟡 Medium — Live subscriber-buffer drops + replay-ring gaps are silent to the client; fixed backoff, no jitter.**
`bus.go:106-113` (16-slot per-subscriber buffer; a burst >16 drops events silently), `bus.go:13,214-218` (100-event replay ring; background beyond 100 loses the middle, logs `events.replay_gap` server-side only). Client has **no gap signal** → shows stale data and can't trigger a reconcile. Backoff is a fixed 3s (`sse-client.ts:119-125`) with no exponential growth or jitter (thundering-herd risk on outage recovery).
**Fix:** enlarge/backpressure the buffer or, on drop/gap, send the client a "resync" control event that triggers a full reconcile; add jitter (and mild growth) to reconnect backoff.

**F5 · 🟡 Medium — Event IDs reset to 0 on restart → replay id collisions.**
`bus.go:82` (`nextID` resets to 0). Post-restart IDs replay numbers a client already saw; combined with F1 the client either stops (204) or mis-orders/de-dupes against stale ids.
**Fix:** epoch-seed `nextID` (per-process prefix or persistent monotonic source), or have the client treat a restart signal as a full-resync trigger.

*(Not blocking today, noted for scale-out:* the bus is entirely in-memory (`InProcessBus`, `bus.go:43-53`) — no durability across restart, and a publish on instance A never reaches a subscriber on instance B. Fine for the single-instance deploy; when scaling horizontally, put a shared transport (Redis pub/sub or equivalent) behind the same `Bus` port.)

### Wave 2 — the download-progress flow (the thing you hit)

**F6 · 🟠 High — Two sources of truth; terminal event drops the item and clears the stage in the same tick, so "Finishing up…" never paints and the row vanishes.**
List membership: `apps/mobile/src/shared/acquisition/activeDownloads.ts:27-35` — `deriveActiveDownloads` includes a track **iff** `acquisition_status === 'pending'` in the `['library-home']` cache (`:28`). Phase caption: `DownloadsBar.tsx:30` / `DownloadsSheet.tsx:22` read `useTrackStage(id)` from the SSE stage store. These are **decoupled**. The `completed` handler (`applyServerEvent.ts:52-63`) patches `status→ready` **and** `clearTrackStage` **and** `invalidateLibrary` all at once. Because `tag`/`store`/`update_track` all collapse into the single **finishing** phase (`stagePhase.ts:11-18`) and fire in milliseconds before `completed`, the 3rd segment lights for ~0 frames, then the item unmounts (status flips off `pending`). `DownloadItem` also carries **no stage** (`activeDownloads.ts:10-15`), so the dock can't show per-track progress even though the SSE stream delivers it.
**Fix:** drive **both** list membership and phase from a **single SSE-fed lifecycle store keyed by `track_id`** (states: `finding|downloading|finishing|done|failed`, seeded by `started`/progress, advanced by `completed → done`). Keep the item mounted through a brief terminal `done ✓` state (~1–1.5s) with a **per-phase minimum dwell** so fast completions still animate finding→downloading→finishing. Stop deriving membership from cache `pending`.

**F7 · 🟠 High — All stage UI is gated on cache `status === 'pending'`, so re-acquire/retry/recovery paths never show stages.**
`apps/mobile/src/features/library/ui/LibraryRow.tsx:105` (`track.acquisition_status === 'pending' ? … : null`), plus `activeDownloads.ts:28` and `DownloadsBar`/`DownloadsSheet` membership. `reconcileForReacquire` (`services/go-api/internal/acquisition/service/acquire.go:178-209`) flips a `ready`/`failed` track back to pending **in the DB**, but there is **no event** telling the client — the cache still says `ready`/`failed`. Progress events then stream (stage store fills), but every display is gated on the cache being `pending`, so **nothing renders**. Retry (`useRetryAcquisition.ts:17-20`) optimistically patches to `pending` (so it shows only the local "Retrying…" label, `LibraryRow.tsx:124`), but missing-file recovery of a silently-gone `ready` file (`acquire.go:189-197`) has **no** optimistic patch and no started event → the row stays `ready` and shows no activity at all.
**Fix:** emit a `track_acquisition_started` (status→pending) event from `scheduler.go`/`acquire.go` when acquisition begins or a track is reverted to pending, patched into caches by `applyServerEvent`; and **ungate the caption** — show it whenever a live stage exists for the track id, regardless of cache status.

**F8 · 🟡 Medium — No `started`/pending SSE event → item appears only via optimistic save or the poll.**
`services/go-api/internal/acquisition/service/acquire.go:85` publishes nothing (`track_acquisition_started` is a `slog` line only). Client appearance depends on `useSaveTrack.ts:46-58` optimistic insert, else the `useLibraryHome.ts:14-19` 5s poll. The dock's `useActiveDownloads.ts:17-22` uses `enabled: false` (never fetches, never drives a poll), and the `refetchInterval` only runs while the **Library screen** is mounted. So a save from Detail without Library open relies entirely on optimistic insert + SSE; if SSE misses/reconnects there's no started event to re-seed membership and nothing polls → stale until the user opens Library and pulls to refresh.
**Fix:** the `started` event from F7 doubles as the server-authoritative appearance trigger (no poll dependency). Alternatively give the dock its own lightweight observer while any track is in-flight.

**F9 · 🟢 Low — DownloadsBar shows a single shared phase for N heterogeneous downloads.**
`DownloadsBar.tsx:29-30,48-51` reads `useTrackStage(items[0].id)` only; heading says "Downloading N songs" but the 3-segment bar reflects just the first item's phase, and when that item completes and drops (F6), `items[0]` becomes a different track at a different phase → segments jump backward.
**Fix:** with the per-id lifecycle store (F6), aggregate the bar's phase (min phase across in-flight items, or a per-phase count).

*(Note: the screen referred to as "actionsheet.tsx" is actually `apps/mobile/src/features/playback/ui/DownloadsSheet.tsx:44`, a plain RN `Modal` — not the `@shared/ui/primitives/ActionSheet` primitive, which is the unrelated library/track menu. Not a sync-gap site.)*

### Wave 3 — patch instead of refetch (event payloads)

**F10 · 🟠 High — `track_added_to_library` carries only `track_id` → forces a refetch though the full track is in hand.**
`services/go-api/internal/catalog/service/add_track.go:85` payload is `{track_id}` only. The client can't render the new row (title/artist/album/artwork/status) → `applyServerEvent.ts:22` must invalidate. The service already holds the fully-populated `*domain.Track` (`add_track.go:76`, returned as `AddTrackOutput.Track`) and the wire shape exists (`TrackResponse`, `track_handler.go:65`).
**Fix:** embed the full track object (mirroring `TrackResponse`) in the payload → client inserts instantly, no refetch.

**F11 · 🟡 Medium — `track_deleted` should be a cache patch, not an invalidate.**
`applyServerEvent.ts:23` invalidates `library-home`, `library`, `playlists` on delete; payload carries `track_id` (`delete_track.go:52`). `patchTrackInCaches` could be extended with remove-by-id to drop the row from all caches instantly. Today a delete on another device triggers three refetches.
**Fix:** add a remove-by-id patch path; drop the invalidate.

**F12 · 🟡 Medium — Acquisition completed/failed patches instantly, then invalidates a 2000-row list anyway.**
`applyServerEvent.ts:61,75` invalidate `['library-home']`, whose query fetches `limit: 2000` (`useLibraryHome.ts:7,13`) — every finished download triggers a full 2000-item refetch though `patchTrackInCaches` already flipped every cache to `ready`. The invalidate exists to re-sync the detail save-control (which reads the library cache imperatively via `useLibraryTracks`) and as a reconciliation backstop.
**Fix:** make the save-control read reactively from `['library-home']` instead of imperatively, then drop the invalidate (or gate it behind a rare reconcile).

### Wave 4 — missing playlist events (silent cross-device)

**F13 · 🟠 High — Playlist rename / remove-track / reorder emit no events; one is even a dead client listener.**
`services/go-api/internal/catalog/service/playlists.go`:
- **G1 rename** — `Rename` (`:107-122`), route `playlist_handler.go:31` `PATCH /playlists/{id}` — no event. `PlaylistDetailScreen.tsx:47-68` (renameMut) optimistically renames + invalidate-only → **never propagates to device B**. Add `playlist_renamed {playlist_id, name}`.
- **G2 remove track** — `RemoveTrack` (`:166-189`), route `playlist_handler.go:34` `DELETE /playlists/{id}/tracks/{trackId}` — no event. **But the client already lists `track_removed_from_playlist` in `INVALIDATION_MAP` (`applyServerEvent.ts:27`) — a dead entry the server never sends.** `PlaylistDetailScreen.tsx:86-108` (removeMut) optimistic + invalidate → cross-device removal doesn't propagate. Add `track_removed_from_playlist {playlist_id, track_id}`.
- **G3 reorder** — `Reorder` (`:191-208`), route `playlist_handler.go:35` `PATCH /playlists/{id}/tracks/reorder` — no event. Add `playlist_reordered {playlist_id, track_ids[]}`.
Note the inconsistency: sibling `AddTrack` **is** pushed (`track_added_to_playlist`, `playlists.go:158`) but its three siblings are not.
**Fix:** emit the three server events with the changed fields; patch them client-side (rename → patch name; remove → remove-by-id; reorder → reorder cached `tracks[]`); remove the dead map entry once the server emits it.

### Wave 5 — remove the polling / staleness crutches (LAST — only safe after Waves 1–4)

**F14 · 🟡 Medium — `useLibraryHome` 5s poll fetching 2000 rows while anything is pending.**
`apps/mobile/src/features/library/hooks/useLibraryHome.ts:14-19` (`refetchInterval = PENDING_POLL_MS = 5000` whenever any track is `pending`, fetching `limit: 2000`). Pure belt-and-suspenders: SSE `progress`/`completed`/`failed` already drive the stage store and cache patches. Not realtime because it distrusts SSE.
**Fix:** once F1 lands, delete the poll (or degrade to a single 30–60s safety net, not a 5s/2000-row loop).

**F15 · 🟡 Medium — Global `staleTime: 30s` background-refetches SSE-covered queries.**
`apps/mobile/src/app/_layout.tsx:55`. Every SSE-covered query (library, playlists, playlist detail) goes stale after 30s and background-refetches on next mount/navigation — a non-realtime reconciliation crutch. (No `refetchOnWindowFocus`/`refetchOnMount` overrides; RN default focus-refetch is off, so the 30s staleTime is the active reconciliation path.)
**Fix:** raise SSE-covered keys to `staleTime: Infinity` and rely on event-driven patch/invalidation. Leave external-read-surface keys (enrichment etc.) as-is.

**F16 · 🟢 Low — Library pull-to-refresh becomes vestigial.**
`LibraryScreen.tsx:84-87` `refresh()` = `state.refetch()` + `pl.invalidatePlaylists()`, threaded into all four grids (`PlaylistsGrid.tsx:100`, `ArtistsGrid.tsx:30`, `AlbumsGrid.tsx:28`, `SongsList.tsx:42`; props at `LibraryScreen.tsx:201,225,244,260`). All `refreshing={false}` (tap-to-refetch, no spinner). It's a manual escape hatch that only exists because membership events are invalidate-only and can lag/be missed. Once F1/F4 + delete/playlist patches (F11/F13) land, membership is fully SSE-driven and this is vestigial.
**Fix:** remove (or keep as a rarely-needed manual reconcile) once realtime membership is trustworthy.

**F17 · 🟢 Low — Mutations rely on `onSuccess`/`onSettled` invalidation redundant with optimistic + SSE.**
- `useSaveTrack.ts:89-92` — `onSettled` invalidates `library`+`library-home` (2000-row) after an optimistic insert already reconciled in `onSuccess:60-67`. Originating device doesn't need it; gate to non-originating.
- `useDeleteTrack.ts:29-34` — optimistic remove + invalidate 4 families; pairs with F11 (patch → drop invalidate).
- `useRetryAcquisition.ts:22-27` — optimistic pending patch + invalidate 4 families; redundant with the patch and the acquisition SSE stream.
- `AddToPlaylistSheet.tsx:59-66` (addMut) — optimistic `track_count++` + invalidate; `track_added_to_playlist` carries `{playlist_id, track_id}` → can patch list `track_count`, but playlist-detail `tracks[]` still needs the row (event lacks it). `createMut` (`:88-92`) invalidate-only.
- `usePlaylistActions.ts:38-41,52-54` — create + `invalidatePlaylists`; `playlist_created` could append a minimal row.
**Fix:** drop the redundant invalidates once the corresponding patches + events exist.

---

## The shape of the fix (two structural moves)

Almost everything reduces to:
1. **Keep the SSE stream open and reliable** — F1 (don't 204), F2 (heartbeat + watchdog), then F3/F4/F5.
2. **Patch caches from events instead of invalidating/polling** — fatten payloads (F10), patch-not-invalidate (F11/F12/F13), and drive the download UI from a **single SSE-fed lifecycle store keyed by `track_id`** (F6/F7/F8). Only then remove the safety nets (F14–F17).

---

## Recommended sequencing

- **Start with Wave 1 (F1 especially) + Wave 2.** This removes the pull-to-refresh feeling *and* fixes the download flow the user hit, in a fairly small, contained change set. Each change with tests, verified on the Expo web build.
- Wave 3 → Wave 4 → Wave 5 as follow-ups. **Wave 5 is strictly last** — do not remove the polls/staleTime crutches until the realtime path they back up is proven.

Per-change discipline: TDD where there's logic (event routing, lifecycle store transitions, payload mapping), `go test ./... && go vet ./...` on backend, `agent-browser`/web-build visual check on frontend flows, and deploy via push-to-main (`deploy-backend` CI). Backend commit scope `adapters`/`application`/`acquire-track`; frontend scope `mobile`. (Heads-up: the local `.husky/commit-msg` hook calls `python`; this machine only has `python3` — either alias it or the hook fails.)

---

## Explicitly NOT problems (correctly scoped out — do not "fix")

- **Discover pull-to-refresh** — `DiscoverBody.tsx:172,184`, `BlendedSection.tsx:118`, `FilteredResults.tsx:68` → `useDiscoverLogic.ts:170`. Discovery search is request/response (ranking recomputed server-side), not a domain stream. Legitimate.
- **External enrichment / detail caches** — 30-min `staleTime` on `useArtistContent.ts`, `useEnrichmentQuery.ts:44`, `useAlbumTracks.ts`, `useRelatedTracks.ts:40`, `useAlbumDiscovery.ts`, `useArtistDiscovery.ts`, `useEnrichResult.ts:23`; 60s on `useAutocompleteSuggestions.ts:12`. Correct caching of external read surfaces.
- **Error-retry `refetch()` buttons** — `AlbumDetailBody.tsx:48`, `useArtistContent.ts:126,225`. Not realtime domains.
- **Playback queue** — client-managed (`shared/playback/queueStore.ts`) with a server snapshot for resume-on-reopen; **no queue/playback SSE event by design** (single active player). Not a gap. (A `queue_state_updated` event would only matter for cross-device "continue listening here" — low priority; see G4 in Appendix A.)
- **Duplicate handling** — no client-side dedup, but `lastEventId` advances per event (`sse-client.ts:136`) so replay won't re-deliver, and every handler is idempotent (`setStage`, `patchTrackInCaches`, invalidations). Fine today; fragile if a non-idempotent handler is ever added.
- **Token refresh** — `useServerEvents.ts:22-29` re-mints via `supabase.auth.getSession()` on each `connect()`; auth checked once at connect (`sse_handler.go:24`). A stream open longer than the token lifetime isn't re-authed mid-stream, but any reconnect re-mints. Low risk (esp. after F1, revisit if streams live for hours).

---

## How to resume (new session)

1. Read TL;DR + this section. Confirm the incident context is closed (the MP3 revert is deployed; all tracks are `.mp3`).
2. Pick a wave (recommended: Wave 1 then Wave 2). Reference findings by id (`F1`…).
3. Backend files: `services/go-api/internal/app/sse_handler.go`, `services/go-api/internal/shared/events/bus.go`, `services/go-api/internal/acquisition/service/{acquire.go,scheduler.go}`, `services/go-api/internal/catalog/service/{add_track.go,delete_track.go,playlists.go}`.
4. Frontend files: `apps/mobile/src/shared/events/{sse-client.ts,useServerEvents.ts,applyServerEvent.ts,trackCachePatch.ts}`, `apps/mobile/src/shared/acquisition/{stageStore.ts,stagePhase.ts,activeDownloads.ts,useActiveDownloads.ts}`, `apps/mobile/src/features/playback/ui/{ActivityDock,DownloadsBar,DownloadsSheet}.tsx`, `apps/mobile/src/features/library/ui/LibraryRow.tsx`, `apps/mobile/src/features/library/hooks/{useLibraryHome,useRetryAcquisition}.ts`, `apps/mobile/src/app/_layout.tsx`.
5. Prod access for verification: SSH key at `~/Downloads/altune-prod.key`, host `altune.duckdns.org` (user `ubuntu`); binary is `/app` in the `altune-go-api` container; logs via `docker compose -f docker-compose.prod.yml logs go-api`; DB via `psql "$DATABASE_URL"` (env in the container).

---

## Appendix A — Backend: every event type + payload (raw)

All publishes go through `events.Publisher.Publish(userId, type, payload)`.

| # | Event type | Published at | Payload | Payload sufficient for client patch? |
|---|---|---|---|---|
| 1 | `track_added_to_library` | `catalog/service/add_track.go:85` | `track_id` only | **No** — forces a fetch (F10). Full `*Track` is in hand at `add_track.go:76` |
| 2 | `track_deleted` | `catalog/service/delete_track.go:52` | `track_id` | Yes (remove-by-id) — but client invalidates instead (F11) |
| 3 | `playlist_created` | `catalog/service/playlists.go:43` | `playlist_id`, `name` | Partial — no `created_at`/track-count/artwork. Minor |
| 4 | `playlist_deleted` | `catalog/service/playlists.go:100` | `playlist_id` | Yes |
| 5 | `track_added_to_playlist` | `catalog/service/playlists.go:158` | `playlist_id`, `track_id` | Partial — no `position`; detail `tracks[]` row not included |
| 6 | `track_acquisition_progress` | `acquisition/service/scheduler.go:200` | `track_id`, `stage` (search/select/download/tag/store/update_track) | Yes |
| 7 | `track_acquisition_completed` | `acquisition/service/acquire.go:231` | `track_id`, `audio_ref` | Partial — `audio_ref` unused by client; corrected `duration_seconds` NOT pushed |
| 8 | `track_acquisition_failed` | `acquisition/service/acquire.go:104` | `track_id`, `reason` | Yes |

**Not on the bus (unrelated):** the `EventType*` constants in `discovery/domain/events.go:42-49` (`search_performed`, `play`, `skip`, `library_add`, `wrong_album`, …) are **inbound behavioral telemetry** ingested at `discovery/adapters/handler/discovery_handler.go:146` (`POST /v1/discovery/events`) → persisted to `discovery_events`. Not client-freshness events.

**State-mutating operations with NO event (the refetch-forcing gaps):**

| Gap | Location | Missing event |
|---|---|---|
| G1 Playlist rename | `catalog/service/playlists.go:107-122`, route `playlist_handler.go:31` | `playlist_renamed {playlist_id, name}` |
| G2 Remove track from playlist | `catalog/service/playlists.go:166-189`, route `playlist_handler.go:34` | `track_removed_from_playlist {playlist_id, track_id}` (client already listens — dead entry) |
| G3 Reorder playlist | `catalog/service/playlists.go:191-208`, route `playlist_handler.go:35` | `playlist_reordered {playlist_id, track_ids[]}` |
| G4 Queue-state save | `playback/service/queue_service.go:29-39`, route `queue_handler.go:26` | `queue_state_updated` (cross-device only; low priority) |
| G5 Status→pending on retry/re-acquire | `acquisition/service/acquire.go:197,203` (`RevertToPending` + `Update`) | `track_acquisition_started`/pending (short window; feeds F7/F8) |

**Corroborating polling evidence:** `GET /tracks/{trackId}/status` (`track_handler.go:41`, `handleGetTrackStatus:218`) — a dedicated point-read status endpoint, the classic shape of a client that polls acquisition progress rather than trusting SSE.

## Appendix B — Backend SSE handler/bus reliability matrix (raw)

| Aspect | Status | Detail |
|---|---|---|
| Last-Event-ID replay | Partial + **BUG (F1)** | Read `sse_handler.go:40-53`; replay `bus.go:188-229` |
| Event history/buffer | Bounded in-memory | Per-user ring, `defaultRingSize=100` (`bus.go:14,71,91-95`); beyond 100 lost, `replay_gap` logged not surfaced (`bus.go:214-218`) |
| Per-user fan-out | Correct | Keyed by `userId.String()` (`bus.go:65-76`); no cross-user leak; multi-device all receive |
| Heartbeat/keepalive | **Missing (F2)** | `sse_handler.go:61-70`, no ticker |
| Events lost while disconnected | Partially recoverable | Within 100-ring + working Last-Event-ID; live-drop of full 16-slot buffer silent (`bus.go:106-113`) until reconnect |
| Durability across restart | **None** | In-memory (`bus.go:43-53`); `nextID`→0 (`bus.go:82`, **F5**) |
| Horizontal scaling | **Single-instance only** | In-process; publish on A never reaches subscriber on B |

## Appendix C — Frontend reliability + coverage detail (raw)

**SSE client reliability:** F1 (204 loop, `sse-client.ts:64-66,82-86,119-125`), F2 (no heartbeat/watchdog), F3 (`responseText` unbounded, `:70-71`), F4 (fixed 3s no jitter `:119-125`; ring-gap no client signal), plus: malformed/unknown data silently dropped (`sse-client.ts:157,162-164`; `applyServerEvent.ts:80`) — violates "never swallow", schema drift would vanish (Low).

**`applyServerEvent` coverage (patch vs invalidate):**

| Event | Handling | Realtime? | Note |
|---|---|---|---|
| `track_acquisition_progress` (`:40`) | direct → `setTrackStage` | instant | ideal |
| `track_acquisition_completed` (`:52`) | direct patch + `invalidateLibrary` (`:61`) | instant but redundant refetch | F12 |
| `track_acquisition_failed` (`:65`) | direct patch + `invalidateLibrary` (`:75`) | instant but redundant refetch | F12 |
| `track_added_to_library` (`:22`) | invalidate | round-trip | payload id-only → F10 |
| `track_deleted` (`:23`) | invalidate ×3 | round-trip | could patch → F11 |
| `playlist_created` (`:24`) | invalidate | round-trip | could append minimal row |
| `playlist_deleted` (`:25`) | invalidate ×2 | round-trip | could patch-remove |
| `track_added_to_playlist` (`:26`) | invalidate ×2 | round-trip | could bump count; detail row absent |
| `track_removed_from_playlist` (`:27`) | invalidate | **never fires** | dead entry — server emits nothing (F13/G2) |

**Full non-realtime seam inventory:** polling F14 (`useLibraryHome.ts:14-19`); staleTime F15 (`_layout.tsx:55`); pull-to-refresh F16 (`LibraryScreen.tsx:84-87` + grids); coarse-field derivation F6 (`activeDownloads.ts:21-35`); mutation invalidates F17 (`useSaveTrack.ts:89-92`, `useDeleteTrack.ts:29-34`, `useRetryAcquisition.ts:22-27`, `AddToPlaylistSheet.tsx:59-66,88-92`, `usePlaylistActions.ts:38-41,52-54`, `PlaylistDetailScreen.tsx:47-68,86-108`).

## Appendix D — Acquisition progress chain trace (raw)

Backend emit: `pipeline.go:38` `reporter.stage(step.Name())` at the top of the loop (fires at step **start**), steps `search,select,download,tag,store,update_track` (`acquire.go:213-222`); `scheduler.go:194-205` publishes `track_acquisition_progress {track_id, stage}`; `acquire.go:230-235` `completed`; `acquire.go:103-108` `failed`; **no `started` publish** (`acquire.go:85` is `slog` only). Timing: `download` (yt-dlp) is the only slow step; `tag`→`store`→`update_track` (all → **finishing** phase) fire back-to-back in ms, immediately followed by `completed`.

Frontend consume/display: `applyServerEvent.ts:40-50` progress→`setTrackStage`; `:52-63` completed→patch `ready` + `clearTrackStage` + `invalidateLibrary`; membership `activeDownloads.ts:27-31` (cache `pending`); caption `DownloadsBar.tsx:30`/`DownloadsSheet.tsx:22`/`LibraryRow.tsx:105` (gated on `pending`). Findings F6/F7/F8/F9 above.
