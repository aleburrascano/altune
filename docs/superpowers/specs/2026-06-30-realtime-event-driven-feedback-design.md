# Realtime, event-driven acquisition feedback — design

**Date:** 2026-06-30
**Status:** Accepted (design)
**Scope:**
- `apps/mobile/` — a new realtime client layer (`shared/realtime/`), the **Activity Dock**, and updates to the save / detail / library / playlist surfaces.
- `services/go-api/` — one new published event (`track_acquisition_progress`); everything else (event bus, SSE transport, terminal events) already exists.

> **Note on scope.** This is deliberately a single, large spec capturing the full vision ("everything realtime"). At planning time it should be **decomposed into phases** (substrate → acquisition UX → background push → broader migration). Specifying it whole keeps the north star coherent; building it whole at once is not recommended.

---

## Problem

The path from saving a track to listening to it gives almost no feedback, and what little it gives is **stale**. Three concrete complaints drove this design:

1. **The silent wait.** Audio acquisition takes ~15–60s (server-side `pending → ready`). The only signal is a 20pt indeterminate spinner on the save button. No progress, no stage, no sense it's working.
2. **"The frontend doesn't reflect the backend, and it's slow."** The app **polls** (`useLibraryHome` refetches every 5s while anything is `pending`). Polling is laggy, wasteful, and scoped to one screen.
3. **The stale-screen bug (verbatim):** *"I added a song to my library and instantly put it into a playlist; even though it was done downloading behind the scenes, the frontend only showed 'pending' until I backed out and could play the song."* The track was `ready` on the backend, but the playlist/detail screens held their own cached `pending` status — only a remount-driven refetch revealed the truth.

All three share one root cause: **there is no live, app-wide channel pushing backend state changes into a single client source of truth.** This spec builds that channel and redesigns the acquisition UX on top of it.

### Goals

- Replace polling with a **push-based realtime channel** so the UI reflects backend state within ~1s of it changing.
- **One source of truth**: a backend event updates a track's state *once* in the shared cache, so every screen showing that track is coherent simultaneously.
- Give the acquisition wait **honest, legible feedback** (stage-based, no fake percentages).
- A calm **"ready"** moment and a continuous **handoff** into listening.
- **Self-healing**: missed events recover on reconnect/foreground; nothing is permanently lost.

### Non-goals

- Bidirectional realtime (chat, presence). The channel is **server → client** only; client actions stay on normal HTTP requests.
- Multi-instance backend fan-out. The event bus is in-process (single server) today; Redis pub/sub is noted as a future upgrade, not built here.
- Real percentage progress. The pipeline does not produce one (see below); we do not invent one.

---

## What already exists (verified in-repo)

| Capability | Status | Evidence |
|---|---|---|
| Async acquisition pipeline (goroutine, ~10min timeout) | ✅ exists | `internal/acquisition/service/acquire.go`, `scheduler.go`, `pipeline.go` |
| Pipeline stages: `search → select → download → tag → store → update` | ✅ exists | `pipeline.go:38` (`reporter.stage(step.Name())`); `job_telemetry.go` |
| In-process per-user event bus with replay ring buffer | ✅ exists | `internal/shared/events/bus.go` |
| Terminal events published | ✅ exists | `acquire.go:231` (`track_acquisition_completed`), `acquire.go:104` (`track_acquisition_failed`) |
| Library/playlist mutation events published | ✅ exists | `add_track.go:85` (`track_added_to_library`), `playlists.go:158` (`track_added_to_playlist`), `delete_track.go:52` (`track_deleted`) |
| **SSE endpoint, auth'd, with `Last-Event-ID` replay** | ✅ **mounted** | `internal/app/app.go:377` → `r.Handle("/events", &sseHandler{bus: a.eventBus})`; `internal/app/sse_handler.go` |
| Supabase JWT auth (`auth.RequireUserID`) on the stream | ✅ exists | `sse_handler.go:24` |
| Client realtime consumption | ❌ **missing** | client still polls — `apps/mobile/src/features/library/hooks/useLibraryHome.ts` (5s `refetchInterval`) |
| Stage progress **published to the bus** | ❌ **missing** | stages reported only into job telemetry, not `events.Publish` |

**Headline:** the substrate is ~80% built. The missing pieces are (a) one new published event on the backend, and (b) the entire client consumption layer.

---

## Part 1 — The realtime substrate (the spine)

### 1.1 Backend: publish stage progress

The pipeline already calls `reporter.stage(name)` per step. Add an event-publishing reporter that emits, per stage transition:

```
event: track_acquisition_progress
data: { "track_id": "...", "stage": "download" }
```

No other backend changes are required for acquisition — `completed`/`failed` already publish. (Reuse the same `events.Publish(userId, ...)` seam; the SSE handler fans it out automatically.)

**Stage → phase mapping** (six technical stages collapse to three user-facing phases; the client owns this mapping so copy changes don't need a deploy):

| Backend stage | User-facing phase | Copy |
|---|---|---|
| `search`, `select` | Finding | "Finding source…" |
| `download` | Downloading | "Downloading…" |
| `tag`, `store`, `update` | Finishing | "Finishing up…" |
| (terminal `completed`) | Ready | "Ready" |
| (unknown / unmapped) | Working | "Working…" (fallback — never crash on a new stage) |

### 1.2 Transport

The existing SSE endpoint at `/events` (auth'd, `Last-Event-ID` replay) is the transport. **Future (not in scope):** swap the in-process bus for an in-memory-ring + Redis pub/sub hybrid (`go-redis/v9`, already a dependency) when a second backend instance is introduced — an ADR-gated change.

### 1.3 Client: one connection, one source of truth

A new `apps/mobile/src/shared/realtime/` module:

- **`RealtimeConnection`** — a single app-wide `EventSource`/SSE client (one connection per app, not per screen).
  - **Lifecycle:** open on authenticated app-foreground; keep the last seen event id; on reconnect send `Last-Event-ID`; close after a grace period in background. Reconnect with exponential backoff + jitter.
  - **Auth:** attach the Supabase JWT; on token refresh or a mid-stream 401, reconnect with a fresh token.
- **`eventDispatcher`** — maps each event `type` to a **cache mutation keyed by id** (the single source of truth). This is the load-bearing piece — it's what makes every screen coherent:

| Event | Cache effect (TanStack Query) |
|---|---|
| `track_acquisition_progress` | Patch the track's `acquisition_stage` wherever the track is cached (by `track_id`). |
| `track_acquisition_completed` | Set `acquisition_status: 'ready'` + `audio_ref` by `track_id`, everywhere. |
| `track_acquisition_failed` | Set `acquisition_status: 'failed'` + `failure_reason` by `track_id`. |
| `track_added_to_library` | Insert/patch the library list (covers cross-device + optimistic reconcile). |
| `track_added_to_playlist` | Patch the affected playlist's track list. |
| `track_deleted` | Remove the track from all cached lists. |

Because the mutation is keyed by `track_id` and applied to the shared cache, **the library row, the playlist screen, the detail screen, and the Activity Dock all reflect the change in the same render** — this is the direct fix for the stale-screen bug.

### 1.4 Reconciliation (self-healing)

Realtime is best-effort; correctness comes from reconciliation:

- **On reconnect:** `Last-Event-ID` replays missed events from the bus ring buffer.
- **On foreground:** invalidate the key queries (library, open playlist, open detail) for a full refetch — covers events older than the ring buffer or dropped while disconnected.
- **Polling becomes a fallback, not the mechanism:** keep a slow safety-net refetch (e.g. 30s) only while the SSE connection is *down*; disable it entirely while connected.

### 1.5 Background / app-closed

SSE does not survive a suspended app. For "it finished while the app was closed," the backend sends an **Expo push notification** on `track_acquisition_completed` (and optionally `failed`). Tapping it deep-links to the track. (Backend push integration + token registration is its own phase.)

---

## Part 2 — The acquisition feedback UX (first consumer of the substrate)

### 2.1 The Activity Dock

One adaptive bottom slot that owns both *acquiring* and *playing*. Four states:

| State | Dock shows |
|---|---|
| **Idle** (nothing playing/downloading) | nothing — clean canvas |
| **Downloading only** | a standalone downloads bar (count + current phase + segmented progress + ⌃ expand) |
| **Playing only** | today's mini-player, unchanged |
| **Both** | downloads bar **stacked above** the mini-player as one unit (tap to collapse the downloads row to a slim chip) |

**Expanded downloads sheet** (tap ⌃): a bottom sheet listing each in-flight track with artwork, current phase, and a 3-segment bar; per-row **cancel (✕)**; finished rows flip to **Ready ▶**; sheet actions **pause-all** and **retry-failed**.

### 2.2 Save tap → stage progress

- Tapping `+` registers optimistically (unchanged entry point).
- The button morphs from today's **indeterminate spinner** into an **indeterminate ring + live stage caption** ("Downloading…"). **No percentage** — the backend has none.
- Inline on the item's row and in the dock: a **3-segment progress bar** that advances by phase (Finding / Downloading / Finishing). Honest sense of motion without a fabricated number.
- Non-blocking: the user keeps browsing; the dock follows them across screens.

### 2.3 Ready

Calm, never noisy, never silent:

- The dock **states** "_X_ ready" in a steady green-edged bar with a **Play** button (no pulse/flash), then returns to whatever's still downloading.
- Inline, the row's check **settles** in; status text → "Ready · tap to play".
- A **toast** fires **only when the finished track is not currently on screen** (context-aware) with a one-tap Play.
- Gentle **haptic** on ready; **sound off** by default.

### 2.4 Handoff into listening

- Tap **Play** → the **artwork glides** (shared-element transition) from the row into the dock/mini-player — the waited-for track visibly becomes the now-playing track.
- **No auto-play, ever** — the user always initiates playback (they may be browsing, queuing several, or in another app).
- **Fallback:** if the native shared-element transition proves fiddly, degrade to "player rises with a brief *Now playing* label + source row marked ▶ Playing."

### 2.5 Failure (rare, but graceful)

On `track_acquisition_failed`: the row and the dock surface "Couldn't download" with a one-tap **Retry** (the retry endpoint/handler already exists — `internal/acquisition/adapters/handler/retry_handler*.go`). Failed items also appear under the sheet's **retry-failed** action.

---

## Part 3 — Broader realtime migration (the vision)

The same channel carries every per-user event, so adopting it for acquisition is step one of moving the whole app off polling/refetch-on-mount:

- **Library & playlist coherence** — `track_added_to_library`, `track_added_to_playlist`, `track_deleted` already fire; wiring them through the dispatcher makes library/playlist views update live (and across devices) without manual invalidation.
- **Future events ride the same seam** — plays, favorites, cross-device queue — each is a new `case` in the dispatcher + (if needed) a new `events.Publish`, never a new transport.

This is the payoff of "event-driven everywhere": **new realtime behavior = one published event + one dispatcher case**, not a new pipeline.

---

## Recommended phasing (for the implementation plan)

1. **Substrate** — `shared/realtime/` connection + dispatcher + reconciliation; wire the *already-emitted* events (`completed`, `failed`, `added_to_library`, `added_to_playlist`, `deleted`); replace `useLibraryHome` polling. **Fixes the stale-screen bug on its own.**
2. **Stage progress** — publish `track_acquisition_progress` on the backend; render the stage caption + segmented bar.
3. **Activity Dock UX** — the adaptive dock, expanded sheet, ready treatment, artwork-glide handoff.
4. **Background push** — Expo push on completion + token registration + deep-link.
5. **Broader migration** — move remaining polling/refetch surfaces onto the dispatcher.

---

## Open questions / risks

- **Verify the SSE route prefix.** `app.go:377` mounts `/events` inside a router group — confirm the full client path (e.g. `/v1/events`) and that the auth middleware wraps it.
- **iOS background SSE.** iOS suspends sockets when backgrounded; we rely on foreground reconciliation + push, not a persistent background connection. Confirm acceptable.
- **JWT expiry mid-stream.** Long-lived SSE outlives a short JWT; the connection must detect 401/expiry and reconnect with a refreshed token.
- **Stage name drift.** The phase mapping must default unknown stages to "Working…" so a backend stage rename never breaks the UI.
- **Ring-buffer horizon.** The bus replay buffer is bounded (per-user ~100 events); foreground reconciliation must cover anything older.
- **Shared-element transition** on React Native may need `react-native-reanimated` shared transitions or a hand-rolled measure-and-animate; the fallback (2.4) de-risks this.

---

## Alignment

- **Quality order (correctness → maintainability → …):** reconciliation guarantees correctness even when realtime drops; the single-dispatcher-by-id design keeps cache coherence maintainable.
- **Hexagonal:** the new backend event reuses the existing `events.Publish` port; no domain→adapter inversion. The SSE handler stays in `app/` (composition root) so `shared/events` never imports auth.
- **Vertical slice:** the realtime client is genuinely cross-feature (library, playlist, detail, playback all consume it) → it earns a home in `apps/mobile/src/shared/realtime/` (2+ real consumers, satisfies the extraction rule).
- **Ubiquitous language:** new terms to add when code lands — `AcquisitionStage`, `track_acquisition_progress` event, `RealtimeConnection`, `Activity Dock`.

## Mockups

Visual exploration that produced this design: `.superpowers/brainstorm/52896-1782816010/content/` (current-flow, feedback-surface, activity-dock, ready-moment, handoff, full-flow, stage-progress-and-coherence).
