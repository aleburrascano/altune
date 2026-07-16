# shared/playback — router

The client-owned Queue: Zustand state machine, resume persistence, playability gating. The native player is a separate concern reached via `PlaybackControls` (provided by `features/playback`).

Invariants:

- `playOrder` is an index **permutation** over `tracks`, never a re-sorted copy; shuffle/reorder/remove mutate ordering without touching `tracks`.
- Native queue index, play-order position, and store `currentIndex` are kept 1:1; `syncCurrentIndex` is the idempotent reconciliation for native-driven changes.
- `canPlay.ts` is the **only** place playability (`AcquisitionStatus === 'ready'`) is checked.
- Feature UIs call the `useQueuePlayback` facade — never the store or native controls directly.

Knowledge base: `okf/mobile/shared-playback.md`; server snapshot: `okf/backend/playback.md` — read before structural work; update in the same commit when behavior it describes changes (pre-commit hook enforces).
