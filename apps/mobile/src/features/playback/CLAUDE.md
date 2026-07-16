# features/playback — router

Owns audio playback end-to-end: mini/full player UI, the native queue, lock-screen controls, behavioral telemetry.

Invariants:

- `react-native-track-player`'s top-level import crashes Expo Go. Provider selection (`hooks/PlaybackProvider.tsx`) uses session-constant `require(...)` — never convert to static imports.
- The **entire** ordered queue is loaded into the native player in one pass (`loadNativeTrack.ts`) so transitions are gapless — no per-track JS cold-loads.
- `positionMs` freezes while backgrounded (identity-stable state object) — the fix for iOS's background-CPU watchdog; don't "fix" the stale progress.
- Native-driven track changes flow back through `service.ts` → queue store `syncCurrentIndex` (idempotent) — never trigger another store-driven skip from them.

Knowledge base: `okf/mobile/playback-feature.md` (+ `okf/mobile/shared-playback.md` for the Queue) — read before structural work; update in the same commit when behavior it describes changes (pre-commit hook enforces).
