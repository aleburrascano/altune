# ADR-0010: Zustand for client-side queue state

- **Status:** Accepted
- **Date:** 2026-06-19
- **Deciders:** solo + Claude
- **Context tags:** [dependency, pattern]

## Context

Queue-based playback requires persistent client-side state that spans screens: the track list, current index, shuffle order, and repeat mode. The existing PlaybackProvider (React Context) manages expo-audio's single-track lifecycle — merging queue orchestration into it would violate single-responsibility and push the component well past 50 lines.

ADR-0005 established TanStack Query for server state. The `rn-state-management.md` rule file designates Zustand for client UI state (theme, player UI, local preferences). Queue state is client UI state — it manages playback order and mode, not server data. The mobile CLAUDE.md requires an ADR before adding a global state library.

## Decision

Add `zustand` as a dependency for the mobile app. Use it for the queue store (`queueStore.ts`) managing: track list, play order (identity or shuffled indices), current index, shuffle toggle, repeat mode (off/all/one), and queue source metadata.

The queue store is consumed by:
- `PlaybackProvider` — subscribes for auto-advance on track end
- `FullPlayer` — reads shuffle/repeat state, exposes prev/next/toggle controls
- `MiniPlayer` — reads hasNext for skip button visibility
- `QueueSheet` — reads the full queue for display
- `useQueuePlayback` — composing hook for screens that initiate queue playback

## Consequences

- **Zustand is the designated tool for client UI state going forward.** Future client-side stores (e.g., user preferences, UI mode) should also use Zustand.
- **PlaybackProvider stays focused on audio.** It calls `play(track)` when the queue store signals a track change — it does not manage track ordering.
- **No middleware initially.** `persist` middleware for queue resume across app restarts is deferred to Phase 9 (server-side persistence is the primary resume mechanism).
