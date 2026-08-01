---
type: Subsystem
title: Shared events (SSE client)
description: Mobile half of the SSE event bus — an XHR-based EventSource replacement with watchdog/recycle/backoff reliability, plus a pure event router that patches TanStack Query caches directly instead of refetching.
resource: apps/mobile/src/shared/events/
tags: [mobile, shared, sse, events, cache-patching]
verified_commit: 98dcd6a8b6c41a7ceeab71d24771c1752819a8eb
---

`SSEClient` (`sse-client.ts`) is a hand-rolled EventSource for React Native (no native EventSource; Hermes/JSC XHR `onprogress` streams `responseText` progressively). Its reliability behaviors came out of the realtime audit: a **heartbeat watchdog** (F2) force-reconnects after `HEARTBEAT_WATCHDOG_MS` (60s) of silence — the server heartbeats every ~25s (see the sse_handler section of [app-wiring](../backend/app-wiring.md)), and a proxy idle-drop just stops `onprogress` with no `onerror`; **response recycling** (F3) reconnects once `responseText` exceeds `MAX_RESPONSE_BYTES` (512KB), since XHR retains the whole stream for the connection's life; **reconnect backoff** (F4) is exponential (1s base, 30s cap) with jitter (`base + random(0, base)` — 1x–2x the base delay), and any received bytes (event *or* heartbeat) reset the attempt counter. Two teardown invariants matter: `connect()` always closes the previous XHR first (AppState/token races otherwise accrue duplicate server connections — the connection-churn bug), and `closeConnection` detaches `onprogress`/`onerror`/`onloadend` *before* `abort()` so an intentional close never feeds `scheduleReconnect`. The `Last-Event-ID` cursor only advances on **id-bearing** events — control events like `resync` carry no id, and blanking the cursor on them would drop replay on the next reconnect. The server side honors this cursor with ring-buffer replay, emits `resync` when the gap was evicted, and deliberately never returns 204 (which tells EventSource to stop reconnecting — a past hot-loop bug); this client is the consumer of all three contracts. `useServerEvents.ts` is the lifecycle wrapper: connect on mount and on AppState `active`, disconnect when backgrounded, dispose on unmount; the token comes from the Supabase session (see [auth-feature](auth-feature.md)), URL is `${apiBase}/v1/events`, and its error handler only logs (`console.warn`) because `SSEClient` owns reconnection — it was an empty function until 2026-07-30, which made a failing token fetch invisible end to end.

`applyServerEvent.ts` is a **pure router** from `ServerEvent` to cache/store effects, extracted from the hook so routing is unit-testable without the transport or AppState. Query keys come from `@shared/lib/query-keys` (`libraryKeys`/`playlistKeys`) so this layer agrees with the feature hooks by import, not string coincidence. A `resync` control event invalidates every SSE-covered query family (`RESYNC_KEYS`: library-home, the featuring prefix, playlists, playlist) — the client cannot patch what it never received, so it fully reconciles. Everything else prefers **direct cache patching over invalidation**: `track_added_to_library` reconstructs a full `TrackResponse` from the payload and upserts it (F10 — the receiving device renders the row instantly), falling back to library-home + featuring invalidates only for older/thin `track_id`-only payloads; it also links the track's `title+artist` into `trackStatusStore`'s identity index (2026-07-26), which is what lets a detail screen opened on a track it does not own resolve an id and go live — without it, a save made anywhere else leaves an already-open screen stale until it is remounted (see [shared-acquisition](shared-acquisition.md#the-identity-index-2026-07-26)); `track_deleted` removes the row from every cache (F11) plus one targeted `playlistKeys.list` invalidate (summary track-counts can't be patched by id alone); acquisition events drive *two* stores at once — the SSE-fed download lifecycle store (`@shared/acquisition/downloadStore`, membership + phase; deliberately **not** the query cache, so a library refetch can't wipe live progress) and `patchTrackInCaches` for `acquisition_status` (started → pending, completed → ready + `audio_ref`, failed → failed + `failure_reason`). `track_acquisition_completed` intentionally fires **no invalidate** (F12) — the patch already flipped every cache, killing the old 2000-row refetch per finished download. Remaining membership events fall through to `INVALIDATION_MAP`; unknown types are recorded via `recordUnhandledEvent` and otherwise ignored, so an event that lands ahead of a client release stays forward-compatible without being invisible (see [the typed contract](#the-event-contract-is-typed-and-drift-is-a-build-failure-2026-07-26)).

`trackCachePatch.ts` is the single source of truth for a Track's live state across three cache families: `libraryKeys.home` (`ListTracksResponse`), every "featuring" list (`ListTracksResponse` under the `libraryKeys.featuringPrefix` `['library','featuring',*]`, one per artist, via `setQueriesData`), and every `playlistKeys.detail(id)` (`PlaylistDetailResponse`, via `setQueriesData` on the `playlistKeys.details` prefix). (A former fourth shape — the `['library']` infinite cache — was patched here long after its owning `useInfiniteQuery` hook was deleted; the 2026-07-16 structure audit removed those ghost branches. The featuring family was added in the follow-up bug fix — without it a retry fired from the FeaturingScreen didn't flip its own visible row until the 60s staleTime.) `patchTrackInCaches` applies a partial update by id everywhere at once — this is what makes one backend acquisition event flip every screen simultaneously instead of one screen invalidating while others show stale `pending`; `removeTrackFromCaches` drops the row from the same three families. `getTrackFromCaches` reads the same caches so the download UI can snapshot title/artist/artwork for events that carry only a `track_id` (returns undefined for a never-cached track, e.g. a save from [detail-feature](detail-feature.md) before the library loaded). All patchers **no-op on unloaded caches** — those fetch fresh anyway; `upsertTrackInCaches` prepends to library-home only (screens re-sort) and leaves playlists and featuring lists untouched — a newly-saved library track is in no playlist, and slotting it into the right featuring list needs its `featured_artists` (the `track_added_to_library` thin-payload path invalidates the featuring family instead). `playlistCachePatch.ts` (F13) does the same for rename/remove-track/reorder from another device, keeping `track_count` consistent in both `playlistKeys.detail(id)` and `playlistKeys.list`; `reorderPlaylistCache` follows the server's full id order and defensively appends any track the event didn't name. Response types come from [shared-api-client](shared-api-client.md); the primary cache consumers are [library-feature](library-feature.md). `__tests__/` pins the contracts: FakeXHR-driven parsing across split chunks, Last-Event-ID replay/preservation through recycle and id-less resync, watchdog and jittered-backoff timing, and per-event-type cache effects. The suite was rebuilt from scratch on 2026-07-30 (see [test-taxonomy](../playbooks/test-taxonomy.md)) — the categories are recorded in `okf/testing/shared-events.md`, and the rebuild is what surfaced the wire-format and cache-consistency defects listed below.

## Patching a query-keyed cache (2026-07-25)

`libraryKeys.home` is gone. The library's track list is now server-filtered and server-sorted, so it is a **family** of caches keyed by `(query, sort)` — and an `InfiniteData<ListTracksResponse>`, not a flat snapshot.

`trackCachePatch` therefore walks pages and writes through `setQueriesData` over the `['library','tracks']` prefix, patching every cached variant at once; `mapPages` is the one place that knows the paged shape. A second flat family, `['library','lookup']`, holds the detail feature's narrow "tracks matching this album/artist" reads and is patched alongside. `replaceTrackInCaches` is new — it swaps an optimistic placeholder for the real row and dedups, which the save mutation used to do itself against the single home snapshot.

Albums and Artists are **aggregates**, so a track-level event cannot be patched into them: `track_added_to_library` and `track_deleted` invalidate `['library','albums']` and `['library','artists']` instead. That is the one place the F10/F11 "patch, never invalidate" rule does not apply, and it is deliberate — a group's track count and artwork cannot be derived from one row's payload.

`applyServerEvent` also writes `@shared/acquisition/trackStatusStore` on every acquisition event. The detail screen reads ownership from the server's stamp on a result and overlays that store for liveness, so the save control still flips the moment a download finishes without the library being resident in memory (see [detail-feature](detail-feature.md)).

## The summary probe is invalidated, not patched (2026-07-25)

`invalidateDerived` (was `invalidateLenses`) now also invalidates `libraryKeys.summary`, the one-row "is the library empty" probe behind the empty-state CTA. Like the album and artist lenses it is derived from the whole collection, so a single track's payload cannot patch it.

It is also the reason that key lives outside `['library','tracks']`: everything under that prefix is swept by `setQueriesData` as `InfiniteData`, and a flat response parked there would crash `mapPages` on the first event that patched it.

## The event contract is typed, and drift is a build failure (2026-07-26)

`ServerEvent.type` used to be a bare `string`, the router ended in `if (!keys) return;`, and a test named *"ignores unknown event types"* asserted that silence was correct. Nothing anywhere related the set of types the Go service publishes to the set the client handles, so a backend deploy could add an event and the mobile app would drop it with a green suite.

`events/eventTypes.ts` now owns `SERVER_EVENT_TYPES`, the single declared vocabulary. Two mechanisms keep it honest:

- **Compile time.** `applyServerEvent` narrows through `isServerEventType` and delegates to a `route` that takes the narrowed union. The tail is `INVALIDATION_MAP`, typed as `Record<InvalidateOnlyEvent, …>` where `InvalidateOnlyEvent` is the union minus every type with an explicit branch. Adding a type to `SERVER_EVENT_TYPES` without handling it therefore fails to compile — the map is missing a key. Verified by adding a fake `track_reacquired` and watching `tsc` reject it.
- **Test time.** `__tests__/eventContract.test.ts` reads the Go source (`git ls-files *.go`, skipping `_test.go`), extracts every `.Publish(…, "name")` literal plus the SSE handler's `event: resync` line, and asserts the two sets match **in both directions** — nothing published goes unhandled, and nothing declared is orphaned. It reads the backend and never writes to it, so it is safe to run while another session is working in `services/go-api`.

Unknown types are recorded in `recordUnhandledEvent` rather than dropped, so a type that appears at runtime ahead of a client release is observable instead of invisible.

## What each event actually updates

The table exists because the gap that bit us was not a routing gap — the router was correct and a screen still went stale. A handler that patches a store is only half of liveness; the other half is a component subscribing to the key that store is written under.

| Event | Query caches | Stores | Screens that must move |
|---|---|---|---|
| `resync` | all `RESYNC_KEYS` | — | every list |
| `track_added_to_library` | tracks, featuring, albums/artists/summary | `trackStatusStore` status + identity link | library lists, detail save control, detail `Source` fact |
| `track_deleted` | all track caches, playlist list | `trackStatusStore` remove | library lists, playlist detail |
| `track_acquisition_started` | track row status | `downloadStore` start, `trackStatusStore` pending | library row phase, Activity Dock, detail save control |
| `track_acquisition_progress` | — | `downloadStore` phase | library row phase, Activity Dock |
| `track_acquisition_completed` | track row status + `audio_ref` | `downloadStore` complete, `trackStatusStore` ready | library row, detail `Source` → `Library`, play source |
| `track_acquisition_failed` | track row status + failure | `downloadStore` fail, `trackStatusStore` failed | library row retry affordance, detail save control |
| `track_replace_failed` | track row back to ready | `downloadStore` fail, `trackStatusStore` ready | library row, Activity Dock |
| `playlist_renamed` | playlist detail name | — | playlist detail hero |
| `playlist_reordered` | playlist detail order | — | playlist detail list |
| `track_removed_from_playlist` | playlist detail | — | playlist detail list |
| `playlist_created` / `playlist_deleted` / `track_added_to_playlist` | invalidate only | — | playlist grid, playlist detail |

The last row is the weak one: invalidation refetches only *active* queries, so it costs a round trip and does nothing while offline. Those three are candidates for direct patching if they ever feel slow.

## A completed acquisition also invalidates cached audio (2026-07-26)

`track_acquisition_completed` has a third effect the table above cannot express, because its target is neither a query cache nor a store: the audio bytes cached on disk. A re-acquired track keeps its object key, so the prefetch file and any pinned download — both keyed by track id — stay resolvable and keep playing the *previous* recording. The handler therefore also calls `invalidateAudioCaches` and `repinIfPinned`.

Both effects are now gated from this side: deleting either call from the handler turns this slice's suite red. That was not true of `repinIfPinned` until 2026-07-30 — the tests exercised the call site but never pinned a track first, so the call always hit its never-pinned early return and could have been removed unnoticed (found while rebuilding `shared/offline`; see [shared-offline](shared-offline.md) for what the re-pin itself guarantees).

`invalidateAudioCaches` dispatches through a registration seam (`shared/acquisition/audioCacheInvalidation`) rather than importing the cache directly, because the prefetch cache lives in `features/playback` and `shared/` may not import a feature. `playbackService()` registers the evictor; when playback never starts nothing is registered, which is correct — there is no cache to clear. Each callback is invoked inside its own try/catch so one failing invalidator cannot break event routing for everything downstream.

`track_replace_failed` exists for the mirror-image reason: replace publishes `track_acquisition_started` exactly as an acquire does, so a failed replace needs a terminal event or the row shows pending forever — but it must not reuse `track_acquisition_failed`, whose handler nulls `audio_ref` and would present a track that is still playing as broken.

## The wire parser was stricter than the SSE spec (2026-07-30)

The blind rebuild of this slice's suite found three framing bugs, all of which had shipped:

- **Block terminator.** `parseChunk` split on bare `\n\n`. A `\r\n\r\n`-terminated block — legal SSE, and producible by any intermediary that normalises line endings — never split, so it sat in `this.buffer` forever and corrupted every byte appended after it. The stream went permanently silent with no error, no reconnect, and a healthy-looking connection. Chunks are now normalised to `\n` on ingest.
- **Multiple `data:` lines.** The spec says a block's `data:` lines are joined with `\n`; `parseBlock` assigned to a single `dataLine`, so all but the last were discarded. Any event whose JSON the server split across lines parsed as the fragment.
- **The optional space.** `line.startsWith('data: ')` required the space that the spec makes optional, so `data:{...}` was dropped entirely. Field parsing now splits on the first colon and strips one optional leading space, and treats a leading-colon line as a comment.

A token failure was equally silent: `connect()` returned without scheduling anything when `getToken()` resolved `null`, and `useServerEvents`'s effect depends only on `queryClient`, so it never re-runs on sign-in. An app that mounted unauthenticated never opened a stream until something backgrounded and foregrounded it. A null token and a rejected token now both go through `scheduleReconnect`, and `onError` — previously an empty function in the hook, which made a broken token fetch invisible end to end — logs via `console.warn`.

## Two caches disagreeing about one number (2026-07-30)

`track_count` is stored twice: on `playlistKeys.detail(id)` and on each item of `playlistKeys.list`. Three defects let them drift, all found by writing the "both caches must agree" assertion rather than by mutation testing:

- `removeTrackFromPlaylistCache` recomputed the detail count from `tracks.length` (idempotent) but decremented the list count unconditionally (not idempotent). A redelivered `track_removed_from_playlist` left the two permanently disagreeing. It now reads the detail cache first and only adjusts the list when the track was actually present, deriving the list count from the detail count when it can.
- `removeTrackFromCaches` dropped the track from a playlist detail's `tracks` but never touched its `track_count`, and `track_deleted` invalidates `playlistKeys.list` only — never `details` — so nothing corrected it. It now recomputes from the surviving array.
- `reorderPlaylistCache` mapped `trackIds` straight to cached tracks with no dedup, so a payload naming an id twice grew the list. The named sequence is deduplicated first.

Two more handlers clobbered good data with `null` on a thin redelivery: `track_acquisition_completed` always wrote `audio_ref`, and `track_acquisition_failed` always wrote `failure_message`. Both now omit the key when the field is absent. `failure_message` matters twice over — the Go publisher at `internal/acquisition/service/acquire.go` sends only `{track_id, reason}` for that event, so the client was reading a field the server has never sent and nulling the REST-supplied value on every failure. `LibraryRow` falls back to "Acquisition failed" when it is null, which is why this was invisible. Note that mutation testing *cannot* find this class: replacing a read of a never-sent field with `null` is an equivalent mutation.

## Two survivors worth killing, three worth explaining (2026-07-30)

The first mutation run over the rebuilt suite left 56 survivors. Triaging them rather than chasing the number found that most were equivalent mutations, and two named real gaps:

- **Nothing asserted the request itself.** Blanking `xhr.open('GET', …)`, the `Authorization: Bearer …` header or `Accept: text/event-stream` all survived. Both headers are hard contracts — the API needs the token, and SSE needs that `Accept` — so a test now pins the method, URL and both headers.
- **An empty progress event re-armed the watchdog.** `if (newText.length > 0)` guards the "bytes arrived, so the stream is alive" reset. Relaxed to `>= 0`, an `onprogress` that delivered no new bytes also reset the 60s watchdog, so a connection that kept firing empty progress events would never be force-reconnected — precisely the silent-stream case the watchdog exists for.

The `fieldOf` survivors are **equivalent, not gaps**, and are recorded here so nobody re-litigates them: removing the leading-colon comment guard, removing the `colon === -1` guard, or changing it to `colon === +1` all produce a field whose `name` is the empty string, and `parseBlock` dispatches only on `name === 'id' | 'event' | 'data'`. Every one of those mutants is therefore unobservable through the public surface. A test that killed them would have to assert on `fieldOf` directly, which is testing the mechanism rather than the behaviour.


## Batch playlist events (2026-08-01)

`tracks_added_to_playlist` and `tracks_removed_from_playlist` join the singular pair, carrying a `track_ids` array of what the server actually applied. They exist because the batch endpoints (see [catalog playlist](../backend/catalog/playlist.md#batch-membership-2026-08-01)) apply one user gesture to N tracks: publishing the singular event N times would have fanned out N identical invalidations of `playlistKeys.details` and `playlistKeys.list` for a single "add 40 tracks" tap.

The added form is invalidate-only, matching its singular counterpart. The removed form patches, looping `removeTrackFromPlaylistCache` over the ids — which keeps the existing `track_count`-recomputed-from-the-array rule and stays replay-idempotent for free, since removing an id that is already gone is a no-op. Non-string entries are filtered out of `track_ids` before the loop, the same guard `playlist_reordered` uses; an `undefined` reaching the patcher would look like a track id and match nothing, which is harmless but hides a malformed payload.

`SERVER_EVENT_TYPES` is now 16 entries. The contract test derives the published set from the Go source, so the two names were checked against `.Publish(…)` literals rather than trusted.
