---
type: Index
title: Mobile features & shared subsystems
description: The Expo app's screen-owning features (apps/mobile/src/features/) and the shared subsystems under apps/mobile/src/shared/.
tags: [index, mobile, expo]
---

Expo (React Native + TypeScript). Features own screens; shared subsystems own cross-feature state and infrastructure.

**The mobile source carries no comments.** Every prose comment and JSDoc block was removed (2026-07-24); intent is expressed by naming and decomposition instead, on the reasoning that a comment is a second source of truth that drifts. Suppressions live in `apps/mobile/eslint.config.js` as named config blocks, not as inline `eslint-disable` pragmas — there are none in source. Durable rationale that code genuinely cannot carry (why a decision was made, what was tried and rejected) belongs here in `okf/` and in the nested `CLAUDE.md` files, which is why those two surfaces are load-bearing rather than supplementary for this app.

## Features

- [app-navigation](app-navigation.md) — Expo Router file-based route tree: auth group, tabbed shell, fullscreen-modal player
- [auth-feature](auth-feature.md) — Supabase sign-in/sign-up/OAuth/password-reset with a single deep-link spine
- [discover-feature](discover-feature.md) — unified search screen: autocomplete, dual-trigger debounce, visibility-confirmed impression/click telemetry
- [detail-feature](detail-feature.md) — read-only track/album/artist detail fed by in-memory handoff, with enrichment and optimistic save
- [library-feature](library-feature.md) — chip-filtered personal collection with client-side grouping and acquisition retry
- [playback-feature](playback-feature.md) — react-native-track-player integration, Expo-Go no-op fallback, native gapless queueing, mini/full player
- [settings-feature](settings-feature.md) — grouped-card settings screen: account, in-app issue reporting, appearance, downloads, and a confirm-gated danger zone

## Shared subsystems

- [shared-playback](shared-playback.md) — client-owned Queue state machine (Zustand) + resume-on-reopen persistence and playability gating
- [shared-api-client](shared-api-client.md) — typed HTTP client for go-api: auth header injection, hand-maintained wire types
- [shared-auth](shared-auth.md) — promoted Supabase client singleton, session-expired signal, sign-out hook (cache-clear invariants)
- [shared-events](shared-events.md) — hand-rolled SSE client (watchdog/recycle/backoff) + pure event router patching TanStack Query caches
- [shared-playlists](shared-playlists.md) — every playlist write plus the picker: batch-only membership and the resolveTrackIds save-on-pick seam
- [shared-favorites](shared-favorites.md) — saved artists/albums/tracks: server-derived identity, optimistic toggle, and the search lift it feeds
- [shared-acquisition](shared-acquisition.md) — SSE-fed download lifecycle store: six pipeline stages → three display phases, forward-only
- [shared-offline](shared-offline.md) — user-pinned offline downloads: document-directory audio, files-win-over-index reconcile, sequential worker
- [shared-telemetry](shared-telemetry.md) — session-id correlation, two-tier reliability outbox, unified recordEvent hook
- [shared-ui](shared-ui.md) — token-based theming (ADR-0008/0009), semantic Theme contract, primitives, motion helpers
- [shared-lib](shared-lib.md) — small pure-utility grab-bag, including the discover→detail in-memory handoff seam

## Platform baseline

Expo SDK 54 with the new architecture enabled (`newArchEnabled: true` in `app.json`), Expo Router file-based routing under `src/app/` with a tabbed shell in `app/(tabs)/`, React 19 and React Native 0.81. `typedRoutes` is on, so route paths are typed. Layouts live in `_layout.tsx`, and the root one wraps the tree in the theme, react-query and error-boundary providers.

Path aliases are configured in both `tsconfig.json` and `babel.config.js`: `@/foo` maps to `src/foo`, `@features/<name>/...` to `src/features/<name>/...`, and `@shared/...` to `src/shared/...`.

Chosen defaults: lists use `FlatList` / `SectionList` (or `FlashList` if performance demands it); images use `expo-image` for caching and remote loading; animation beyond `Animated` basics uses `react-native-reanimated`. Server state is React Query and local state is `useState` / `useReducer` / context — no global state library without an ADR. Sensitive values go in `expo-secure-store`; structured non-sensitive data in `expo-sqlite` and key/value in `AsyncStorage`. Testing is Jest with the `jest-expo` preset and `@testing-library/react-native` (pinned to 13.x — 14.x requires a `test-renderer` peer that does not wire up under the SDK 54 preset), with unit and component tests in `__tests__/` next to their source; Maestro is preferred over Detox for e2e. Which tests a slice deserves comes from [test-taxonomy](../playbooks/test-taxonomy.md), and the per-slice verdict is committed under `okf/testing/`.

**The suite was deleted and is being rebuilt (2026-07-30).** A mutation audit of six `src/shared/` subsystems ran 72 mutations against 552 passing tests and killed 26 — a 38% kill rate, meaning roughly two thirds of what the suite executed it did not constrain. The cause was not carelessness: three pieces of infrastructure hid untested code. Module-wide `jest.mock` of a whole file removed it from execution entirely; the `expo-file-system` double had `write() {}` unwired from `textSync()`, so no test could observe persistence at all and both durability guarantees in the app were unassertable; and a single global `coverageThreshold` blended per-file zeros into an average nobody inspected. The doubles now live in `jest/doubles/` (filesystem, secure-store, track-player), each round-trips, each takes injected failures, and `__tests__/harness.test.ts` constrains the doubles themselves. Coverage floors in `jest.config.js` are per-glob and raise-only, one entry per slice, and `.github/workflows/test-mobile.yml` gates them — before this, nothing ran the mobile suite in CI at all. Kill rate is measured by Stryker (`npm run mutate`, nightly via `mutation-mobile.yml`), which is the reproducible number the ratchet stands on; the `test-assassin` agent stays for the semantic mutations no generator produces — a swapped adjacent field, a reversed spread, a commit before its send. See [ci-cd-pipeline](../playbooks/ci-cd-pipeline.md) for both gates and [test-taxonomy](../playbooks/test-taxonomy.md) for what a slice's tests must prove. Debugging uses React DevTools, optionally `react-native-flipper`, and the `__DEV__` global for dev-only paths.

Adding a native module may require `expo prebuild` if it is not in the managed pre-built clients, which is worth an ADR when it happens.
