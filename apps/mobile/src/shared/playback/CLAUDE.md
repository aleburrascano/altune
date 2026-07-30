# shared/playback — router

The client-owned Queue: Zustand state machine, resume persistence, playability gating. The native player is a separate concern reached via `PlaybackControls` (provided by `features/playback`).

Invariants:

- `playOrder` is an index **permutation** over `tracks`, never a re-sorted copy; shuffle/reorder/remove mutate ordering without touching `tracks`.
- Native queue index, play-order position, and store `currentIndex` are kept 1:1; `syncCurrentIndex` is the idempotent reconciliation for native-driven changes.
- `trackKey.ts` is the only definition of a `PlaybackTrack`'s identity; when a native index and a `trackKey` disagree, the key wins.
- `canPlay.ts` is the **only** place playability (`AcquisitionStatus === 'ready'`) is checked; no other file in this slice may contain the `'ready'` literal.
- Feature UIs mutate the Queue only through the `useQueuePlayback` facade; nothing outside `shared/playback/` and `features/playback/` imports `useQueueStore`.
- `loadQueue` and `restoreQueue` both land `currentIndex` at `-1` when the resulting play order is empty.
- `RESTART_THRESHOLD_MS` is never restated as a literal by a consumer.

Tests: `__tests__/queueStore.lifecycle.test.ts`, `queueStore.navigation.test.ts`, `queueStore.editing.test.ts`, `queueStore.property.test.ts`, `useQueuePlayback.test.tsx`, `canPlay.test.ts`, `isCurrentlyPlaying.test.ts`, `trackKey.test.ts`, `previewUrl.test.ts`, `toPlaybackTrack.test.ts`, `playFromList.test.ts`, `crossSurfaceContract.test.ts`, `playbackContext.test.tsx`, `invariants.test.ts`. Selection record and mutation result: `okf/testing/shared-playback.md`.

Knowledge base: `okf/mobile/shared-playback.md`; server snapshot: `okf/backend/playback.md` — read before structural work; update in the same commit when behavior it describes changes (pre-commit hook enforces).
