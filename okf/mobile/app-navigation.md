---
type: Mobile Feature
title: App navigation (Expo Router)
description: File-based route tree — auth group, tabbed shell, nested per-tab stacks, and a fullscreen-modal player group — wired at the root layout.
resource: apps/mobile/src/app/
tags: [mobile, feature, navigation, expo-router, routing]
verified_commit: 98dcd6a8b6c41a7ceeab71d24771c1752819a8eb
---

The file-based route tree (Expo Router) that composes every feature into the running app. `_layout.tsx` (root) is the composition point: it holds the single `QueryClientProvider` (ADR-0005 — every feature's hooks inherit this one client, configured with 30s `staleTime` and a custom `retry` predicate delegating to `isRetryable` from [shared-api-client](shared-api-client.md)), `ThemeProvider` (dark-only v1, ADR-0008, see [shared-ui](shared-ui.md)), font-loading gated behind the native splash screen (prevents a FOUT flash), and Android nav-bar dark-forcing (re-applied on every `AppState` "active" to kill a resume-time white flash). It conditionally `require`s `registerPlaybackService` only outside Expo Go, mirroring the same native-module-avoidance pattern as the [playback-feature](playback-feature.md) itself. `AuthGate` (from [auth-feature](auth-feature.md)) wraps the entire routed tree, with `ServerEventsBridge` and `AuthDeepLinkBridge` mounted as null-rendering components inside it so SSE and deep-link subscriptions only run once a session exists. The root `<Stack>` has four screens: `(tabs)`, `(auth)`, `reset-password` (a top-level route AuthGate deliberately lets through during password recovery), and `player` (presented as a `fullScreenModal` with `slide_from_bottom`).

**`(auth)/_layout.tsx`** lives outside AuthGate's redirect scope so signed-out users can reach it. It draws the blurred artwork background once behind a transparent `fade`-animated `Stack`, so navigating sign-in ↔ sign-up ↔ forgot-password never remounts (and re-flashes) the background.

**`(tabs)/_layout.tsx`** is the tabbed shell (Discover/Library/Settings) using a custom `TabBar`; the `ActivityDock` (the unified bottom dock that stacks the in-flight-downloads bar above the now-playing `MiniPlayer`) is composed inline here — the layout is the composition root, so it's the one place allowed to import both the acquisition download views ([shared-acquisition](shared-acquisition.md)) and the playback `MiniPlayer` ([playback-feature](playback-feature.md)), neither of which may import the other — and rendered above the tab bar via the `tabBar` render-prop so it's visible across all three tabs whenever a track is loaded or downloads are in flight. Each tab directory (`discover/`, `library/`) has its own nested `_layout.tsx` wrapping a `Stack` in a `ScreenBoundary` error boundary, giving each tab independent, unlimited-depth navigation (discover → artist → album → track → ...) with natural back-button behavior; `discover/index.tsx` and `library/index.tsx` are thin re-exports of the feature's screen component, and both tabs' `detail.tsx` render the same shared `DetailScreen` (see [detail-feature](detail-feature.md)). Both tab stacks also carry a `featuring.tsx` route — thin re-exports of the library feature's `FeaturingScreen` ("everything featuring X", params `name`, `mbid?`, `deezer_id?`) — duplicated per stack deliberately, so tapping a featured artist on a track detail pushes within the *current* tab's stack and the back button returns to that detail rather than jumping tabs.

**`player/_layout.tsx`** nests a second `Stack` inside the root's single `fullScreenModal` screen: `index` (`FullPlayer`) is the base, and `queue` (`QueueSheet`) pushes on top as a `modal` with `slide_from_bottom` — necessary because without this nested layout, expo-router would expose flat `player/index`/`player/queue` routes that the root's single `<Stack.Screen name="player">` couldn't match.

**`index.tsx`** is a bare `<Redirect href="/discover" />` — route groups are path-transparent, so `/discover` resolves to `(tabs)/discover`, making Discover the true landing surface.

Key files: `_layout.tsx`, `(auth)/_layout.tsx`, `(tabs)/_layout.tsx`, `index.tsx`, `player/_layout.tsx`, `(tabs)/discover/_layout.tsx`, `(tabs)/library/featuring.tsx`.

## Error boundaries and root bridges (2026-07-24)

`ScreenBoundary` previously wrapped only the three `(tabs)` stacks, so a render throw anywhere else escaped to the root and blanked the app. It now also wraps:

- the **player** group (`app/player/_layout.tsx`) — its own route group, holding `FullPlayer`, the queue sheet and the lyrics sheet
- the **`(auth)`** group — around the Stack only, so `ArtworkBackground` survives; this is the one group a signed-out user cannot navigate away from
- a **root backstop** in `app/_layout.tsx`, catching what the group boundaries cannot: root-level routes like `reset-password`, and throws in the group layouts themselves. Nearest boundary still wins, so this only fires for gaps.

`app/player/lyrics.tsx` is a new modal route in the player stack (see [playback-feature](playback-feature.md)).

Two bridges mount inside `PlaybackProvider` at the root because they must outlive the screens that configure them: `SleepTimerBridge` (pauses playback when the timer expires, with the player screen closed) and `OfflineReconcileBridge` (rebuilds the pinned-download index from disk once per launch).

## Retry policy (2026-07-25)

The root `QueryClient`'s `retry` predicate now delegates to `isRetryable` (see [shared-api-client](shared-api-client.md)) and allows 5 attempts instead of 3.

It previously read "retry anything that is not an `ApiError` 401". The 401 exclusion was right in spirit — a 401 is a verdict, not a hiccup, and retrying only delays the error the user needs to see — but `apiFetch` was reporting *failed session refreshes* as 401, so on a weak connection every query on the screen hit the one branch guaranteed not to retry and the library rendered empty until the app was restarted. The predicate is now positive rather than negative: retry `NetworkError` plus `429`/`5xx`, and nothing else. That also stops the old behaviour of burning three round trips on a `404`, which was never going to succeed either.

The attempt cap rose to 5 because the errors now reaching it are transient by construction, and React Query's exponential backoff means five attempts span roughly half a minute — enough to ride out a tunnel or a lift, which three attempts were not.
