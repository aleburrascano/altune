---
type: Mobile Feature
title: App navigation (Expo Router)
description: File-based route tree — auth group, tabbed shell, nested per-tab stacks, and a fullscreen-modal player group — wired at the root layout.
resource: apps/mobile/src/app/
tags: [mobile, feature, navigation, expo-router, routing]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

The file-based route tree (Expo Router) that composes every feature into the running app. `_layout.tsx` (root) is the composition point: it holds the single `QueryClientProvider` (ADR-0005 — every feature's hooks inherit this one client), `ThemeProvider` (dark-only v1, ADR-0008, see [[shared-ui]]), font-loading gated behind the native splash screen (prevents a FOUT flash), and Android nav-bar dark-forcing (re-applied on every `AppState` "active" to kill a resume-time white flash). It conditionally `require`s `registerPlaybackService` only outside Expo Go, mirroring the same native-module-avoidance pattern as the [[playback-feature]] itself. `AuthGate` (from [[auth-feature]]) wraps the entire routed tree, with `ServerEventsBridge` and `AuthDeepLinkBridge` mounted as null-rendering components inside it so SSE and deep-link subscriptions only run once a session exists. The root `<Stack>` has four screens: `(tabs)`, `(auth)`, `reset-password` (a top-level route AuthGate deliberately lets through during password recovery), and `player` (presented as a `fullScreenModal` with `slide_from_bottom`).

**`(auth)/_layout.tsx`** lives outside AuthGate's redirect scope so signed-out users can reach it. It draws the blurred artwork background once behind a transparent `fade`-animated `Stack`, so navigating sign-in ↔ sign-up ↔ forgot-password never remounts (and re-flashes) the background.

**`(tabs)/_layout.tsx`** is the tabbed shell (Discover/Library/Settings) using a custom `TabBar`; `MiniPlayer` is rendered above it via the `tabBar` render-prop so it's visible across all three tabs whenever a track is loaded. Each tab directory (`discover/`, `library/`) has its own nested `_layout.tsx` wrapping a `Stack` in a `ScreenBoundary` error boundary, giving each tab independent, unlimited-depth navigation (discover → artist → album → track → ...) with natural back-button behavior; `discover/index.tsx` and `library/index.tsx` are thin re-exports of the feature's screen component, and both tabs' `detail.tsx` render the same shared `DetailScreen` (see [[detail-feature]]).

**`player/_layout.tsx`** nests a second `Stack` inside the root's single `fullScreenModal` screen: `index` (`FullPlayer`) is the base, and `queue` (`QueueSheet`) pushes on top as a `modal` with `slide_from_bottom` — necessary because without this nested layout, expo-router would expose flat `player/index`/`player/queue` routes that the root's single `<Stack.Screen name="player">` couldn't match.

**`index.tsx`** is a bare `<Redirect href="/discover" />` — route groups are path-transparent, so `/discover` resolves to `(tabs)/discover`, making Discover the true landing surface.

Key files: `_layout.tsx`, `(auth)/_layout.tsx`, `(tabs)/_layout.tsx`, `index.tsx`, `player/_layout.tsx`, `(tabs)/discover/_layout.tsx`.
