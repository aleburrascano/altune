---
type: Mobile Feature
title: Playback (mobile)
description: react-native-track-player integration with an Expo-Go-compatible no-op fallback, native gapless queueing, and mini/full player UI.
resource: apps/mobile/src/features/playback/
tags: [mobile, feature, playback, track-player, expo-go, background-audio]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

Owns audio playback end-to-end: mini/full player UI, the native queue, lock-screen controls, and behavioral telemetry. The central design constraint is that `react-native-track-player`'s top-level import touches a native module Expo Go doesn't bundle — importing it unconditionally would crash the app in Expo Go.

**Provider selection** (`hooks/PlaybackProvider.tsx`): a session-constant `isExpoGo` flag chooses which provider implementation to `require(...)` — `expoGoPlaybackProvider.tsx` (no-op, audio inert, `console.warn`s on play) or `trackPlayerProvider.tsx` (the real backend) — using `require` rather than a static `import` specifically so the unused module's native import is never touched. Because the choice is constant for the session, there's no hook-order risk from branching. `expoGoPlaybackProvider` still drives the Zustand queue store (see [[shared-playback]]) on skip so screens remain testable without a dev build.

**TrackPlayerPlaybackProvider** (`hooks/trackPlayerProvider.tsx`) is the real implementation: it derives a `PlaybackState` (`idle|loading|playing|paused|ended|error`) from `usePlaybackState`/`useProgress`, freezing `positionMs` while backgrounded (`useIsForeground`) so the memoized state object keeps its identity and no scrubber re-render or JS-thread animation runs in the background — the fix for iOS's background-CPU watchdog. `play`/`startQueue` delegate to `loadNativeTrack`/`loadNativeQueue` (`loadNativeTrack.ts`), which load the **entire** ordered queue into the native player in one pass so TrackPlayer prefetches the next track and transitions are gapless — no per-track JS cold-load. `service.ts` (`playbackService`, registered outside Expo Go in the root layout) forwards lock-screen remote events (`RemotePlay`/`RemotePause`/`RemoteNext`/`RemoteSeek`) to the native player and mirrors `PlaybackActiveTrackChanged` back into the Zustand queue store so UI stays in lockstep with native/background/lock-screen skips.

**Behavioral signals**: `signals.ts` holds pure helpers — `listenThresholdMs` (30s OR 50% of duration, Kim WSDM 2014) and `buildTrackPayload` — consumed by `usePlaybackSignals` to fire `play`/`completed`/`skip` events without polluting the provider with telemetry logic (see [[telemetry]] on the backend side).

**UI**: `ui/MiniPlayer.tsx` sits above the tab bar (docked in `(tabs)/_layout.tsx`) with an animated progress bar; `ui/FullPlayer.tsx` is the fullscreen modal (shuffle/repeat/skip, scrubber, retry-on-error); `ui/QueueSheet.tsx` shows Now Playing + swipe-to-remove Up Next list backed by the queue store.

Key files: `hooks/PlaybackProvider.tsx`, `hooks/trackPlayerProvider.tsx`, `hooks/expoGoPlaybackProvider.tsx`, `service.ts`, `signals.ts`, `loadNativeTrack.ts`, `ui/FullPlayer.tsx`, `ui/MiniPlayer.tsx`, `ui/QueueSheet.tsx`.
