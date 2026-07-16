---
type: Index
title: Mobile features & shared subsystems
description: The Expo app's screen-owning features (apps/mobile/src/features/) and the shared subsystems under apps/mobile/src/shared/.
tags: [index, mobile, expo]
---

Expo (React Native + TypeScript). Features own screens; shared subsystems own cross-feature state and infrastructure.

## Features

- [app-navigation](app-navigation.md) — Expo Router file-based route tree: auth group, tabbed shell, fullscreen-modal player
- [auth-feature](auth-feature.md) — Supabase sign-in/sign-up/OAuth/password-reset with a single deep-link spine
- [discover-feature](discover-feature.md) — unified search screen: autocomplete, dual-trigger debounce, visibility-confirmed impression/click telemetry
- [detail-feature](detail-feature.md) — read-only track/album/artist detail fed by in-memory handoff, with enrichment and optimistic save
- [library-feature](library-feature.md) — chip-filtered personal collection with client-side grouping and acquisition retry
- [playback-feature](playback-feature.md) — react-native-track-player integration, Expo-Go no-op fallback, native gapless queueing, mini/full player
- [settings-feature](settings-feature.md) — account screen: profile card, featured-artist backfill trigger, sign-out

## Shared subsystems

- [shared-playback](shared-playback.md) — client-owned Queue state machine (Zustand) + resume-on-reopen persistence and playability gating
- [shared-api-client](shared-api-client.md) — typed HTTP client for go-api: auth header injection, hand-maintained wire types
- [shared-auth](shared-auth.md) — promoted Supabase client singleton, session-expired signal, sign-out hook (cache-clear invariants)
- [shared-events](shared-events.md) — hand-rolled SSE client (watchdog/recycle/backoff) + pure event router patching TanStack Query caches
- [shared-acquisition](shared-acquisition.md) — SSE-fed download lifecycle store: six pipeline stages → three display phases, forward-only
- [shared-telemetry](shared-telemetry.md) — session-id correlation, two-tier reliability outbox, unified recordEvent hook
- [shared-ui](shared-ui.md) — token-based theming (ADR-0008/0009), semantic Theme contract, primitives, motion helpers
- [shared-lib](shared-lib.md) — small pure-utility grab-bag, including the discover→detail in-memory handoff seam
