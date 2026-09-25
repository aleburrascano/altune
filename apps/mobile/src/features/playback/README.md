# playback — seam map

Audio playback on top of `react-native-track-player` (TrackPlayer). The public face is the
`PlaybackContext` in `@shared/playback`; this folder is its native implementation. Entry points:

- `hooks/PlaybackProvider.tsx` — mounted in `src/app/_layout.tsx`, supplies the context.
- `registerPlaybackService.ts` → `service.ts` — the headless TrackPlayer service (remote
  controls, native events), registered from `src/app/_layout.tsx` outside Expo Go.

Queue _state_ lives in `@shared/playback/queueStore`; this folder keeps the native queue in step
with it.

## 1. Stores

Small zustand stores, no native calls.

- `playbackErrorStore.ts` — last playback error, keyed by track so the UI shows it only for that track.
  Each error carries a typed `kind` (network, auth, not_found, decode, queue drift, unknown),
  classified by `classifyPlaybackError.ts` from the native error code or API error, so callers
  branch on it, not the message.
- `playbackRateStore.ts` — selected playback speed.
- `sleepTimerStore.ts` — sleep timer deadline (fired by `ui/SleepTimerBridge.tsx`).

## 2. Native bridge

Everything that talks to TrackPlayer. The rule: **every mutation of the native queue goes
through `withNativeQueue`**, and **every full load claims a `loadToken`**. The two "native +
queue/sync" files are unrelated: `nativeQueueLock` orders outgoing commands, `nativeSyncGuard`
filters one incoming event.

Each file and the race/event it guards against:

- `nativeQueueLock.ts` — interleaved TrackPlayer queue calls (reset/add/skip/remove from a
  load, a user skip, a remote-control skip and a prefetch swap all at once); serializes them on
  one promise chain, with a 15 s per-op deadline so a stalled bridge call cannot wedge the chain.
- `nativeSyncGuard.ts` — the spurious index-0 `PlaybackActiveTrackChanged` fired while
  `loadNativeQueue` adds tracks to an empty queue before `skip` reaches the real start index;
  suppresses it so the store cursor never flickers to track 0 (per-load generations, so
  overlapping loads cannot leave a stale suppression behind).
- `nativeTrack.ts` — mismatched native item identity: maps a `PlaybackTrack` to a TrackPlayer
  item whose `id` is always `trackKey(track)`, which `service.ts` and `nativeTrackSwap.ts` rely
  on to match the active native item back to the queue.
- `loadNativeTrack.ts` — the load operations (`loadNativeTrack`, `loadNativeQueue`, reorder,
  append, insert-next); guards against a superseded load touching the native queue after a
  newer load started (checks its token at every await boundary).
- `loadToken.ts` — the superseded-load race itself: a monotonic token; `isStale` tells an
  in-flight load a later one has claimed the player.
- `presignWindow.ts` — a long session running past the signed-URL window: slides the
  `MAX_PRESIGN` presigned block forward as the active track nears its edge.
- `initPlayer.ts` — concurrent `setupPlayer` calls: all callers share one setup promise, and a
  failed attempt is dropped so a later call retries instead of replaying the stale rejection.
- `audioPrefetch.ts` — duplicate, late, stalled or superseded prefetch downloads: an in-flight
  map dedupes a track, a newer prefetch for a different next track aborts the old download, a
  15 s no-progress timeout and a per-file byte cap abandon a download, and the swap only happens
  if the downloaded track is still next once the download settles. A remote kill switch
  (`AUDIO_PREFETCH_ENABLED` on the API, reported as `prefetch_enabled` on every audio-url
  response) turns `prefetchNext` into a no-op so every track streams.
- `audioCache.ts` — cache growth: on-disk prefetch files keyed `<trackId>.<version>`, evicting
  everything outside the current track plus the next few, then the farthest of those while the
  cache is over its total byte cap.
- `playbackHealth.ts` — silent prefetch/presign degradation (both fall back to streaming): tallies
  prefetch outcomes by failure stage and presign outcomes, and sends them as one aggregate
  `playback_health` telemetry event per 25 outcomes or when the app backgrounds.
- `nativeTrackSwap.ts` — swapping the wrong native item: replaces a track with its local file
  only while it is still _upcoming_ (never the playing item), and `repairActiveToStreaming`
  reloads a failed local file only if that track is still the active one.
- `createNativePlaybackActions.ts` — the command set behind the context (`play`, `startQueue`,
  skips, remove, seek, retry); routes queue-mutating commands through `withNativeQueue` and
  never lets a native rejection crash a UI handler; a failed queue mutation is classified
  (transient vs permanent drift) and reported on the displayed track, whose `retry` rebuilds
  the native queue from the store.
- `seekControls.ts` — native seek leaving the player paused: re-issues `play` if it was playing.
- `service.ts` / `registerPlaybackService.ts` — native events arriving from outside the app
  (remote controls, audio ducking, playback errors, active-track changes); applies the
  `nativeSyncGuard` filter before syncing the store cursor.

## 3. Hooks / providers (`hooks/`)

- `PlaybackProvider.tsx` — the dual-provider split: lazily `require`s
  `trackPlayerProvider.tsx` in a dev/prod build, or the no-op `expoGoPlaybackProvider.tsx` in
  Expo Go, where the TrackPlayer native module does not exist (importing it would crash).
- `trackPlayerProvider.tsx` — composes the real context value from the hooks below and
  `createNativePlaybackActions`.
- `usePlaybackPosition.ts`, `usePlaybackSignals.ts`, `useQueueResume.ts`, `useLyrics.ts`,
  `useAppStateChange.ts`, `useIsForeground.ts` — position, telemetry, queue save/restore,
  lyrics, and app-state hooks.

Pure helpers the hooks and UI consume (no native calls): `derivePlaybackState.ts`,
`signals.ts`, `queueStateWire.ts`, `queueRebuildStrategies.ts`, `resumeQueue.ts`,
`lyrics-sync.ts`, `queueItem.ts`, `queueMenuOptions.ts`, `clamp.ts`.

## 4. Presentation (`ui/`)

Player screens and sheets: `MiniPlayer`, `FullPlayer`, `QueueSheet`/`QueueRow`, `LyricsSheet`,
`PlayerOptionsSheets` (with `SleepOptions`/`SpeedOptions`), `Scrubber`, `SheetHeader`, and
`SleepTimerBridge` (pauses playback when the sleep timer fires).
