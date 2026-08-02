# features/playback — router

Owns audio playback end-to-end: mini/full player UI, the native queue, lock-screen controls, behavioral telemetry.

Invariants:

- `react-native-track-player`'s top-level import crashes Expo Go. Provider selection (`hooks/PlaybackProvider.tsx`) uses session-constant `require(...)` — never convert to static imports.
- The **entire** ordered queue is loaded into the native player in one pass (`loadNativeTrack.ts`) so transitions are gapless — no per-track JS cold-loads.
- `positionMs` freezes while backgrounded (identity-stable state object) — the fix for iOS's background-CPU watchdog; don't "fix" the stale progress.
- Native-driven track changes flow back through `service.ts` → queue store `syncCurrentIndex` (idempotent) — never trigger another store-driven skip from them.
- Every native entry carries `id: trackKey(track)`; anything that reconciles or mutates a native slot resolves it by that id, never by a remembered index.
- Never attribute a `PlaybackError` to the store's current track — resolve the failing one from `getActiveTrack()`.
- Playback errors live in `playbackErrorStore`, keyed by `trackKey` — never clear the store on a track change.
- Every mutation of the native queue (load, reset, add, remove, reorder, skip, prefetch swap) runs inside `withNativeQueue` (`nativeQueueLock.ts`); network work is resolved *before* taking the lock, never inside it.
- A native entry always carries an `artwork` — `nativeTrack.ts` substitutes `assets/artwork-placeholder.png` when a track has none, so the lock screen can never keep the previous track's cover.
- `initPlayer.ts` sets up the player with `autoHandleInterruptions: true`; never add a `RemoteDuck` handler alongside it.

Tests: none yet — this slice's suite was reset on 2026-07-30 and is rebuilt per `okf/playbooks/test-taxonomy.md`, with the per-category verdict committed to `okf/testing/<slice>.md`.

Knowledge base: `okf/mobile/playback-feature.md` (+ `okf/mobile/shared-playback.md` for the Queue) — read before structural work; update in the same commit when behavior it describes changes (pre-commit hook enforces).
