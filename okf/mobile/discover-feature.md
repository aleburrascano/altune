---
type: Mobile Feature
title: Discover
description: Unified multi-provider search screen with autocomplete, dual-trigger debounce, and visibility-confirmed impression/click telemetry.
resource: apps/mobile/src/features/discover/
tags: [mobile, feature, discover, search, telemetry, react-query]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

The unified music-search surface (`docs/specs/discover-music-v1`, ADR-0007, restyled ADR-0009), talking to the backend's [[scatter-gather]] search pipeline. `ui/DiscoverScreen.tsx` renders a title, a `SearchBar` with as-you-type debounce, and a body driven by a five-state pure union `DiscoverView = loading|empty-no-query|results|zero-results|full-error` (`state.ts`'s `_viewForState`). Empty-no-query shows the user's last-10 distinct searches (`useSearchHistory`). All orchestration is lifted out of the screen into `hooks/useDiscoverLogic.ts` — DiscoverScreen is a thin presentational shell over its returned state.

**Search commit**: `hooks/useDebouncedSearch.ts` is the dual-trigger machine — `onChangeText` auto-commits `inputValue` to `committedQuery` after a 300ms debounce (min 2 chars) with `isExplicitSubmit: false`; Enter key or a history tap bypasses the timer and commits immediately with `isExplicitSubmit: true`. `hooks/useDiscoverSearch.ts` keys its `useQuery` on the trimmed `committedQuery` and passes `saveHistory` through, so only explicit submits persist server-side history — debounced partial queries don't bloat it. It also cancels any in-flight superseded search on each new commit so fast typing doesn't leave several full searches running server-side.

**Blended results view**: for the "All" filter, `state.ts` provides pure helpers `_groupByKind`, `_topResult` (results[0] as a Top Result card), `_sectionOrder` (the kind whose strongest member ranks earliest shows first — a track query surfaces Songs before Artists), and `_cap` (10 per section). A kind chip (`All · Albums · Songs · Artists`) filters to a flat list of one kind.

**Telemetry**: `impressions.ts` (`buildImpressionRows`) is a pure mapper from the rendered result slate to `{result_signature, position, provider, confidence}` rows; `hooks/useImpressionLogger.ts` gates emission on FlatList's `onViewableItemsChanged` (≥50% visible) and fires exactly one `results_shown` event per `search_id` — a visibility-confirmed impression, distinct from the server's "what was returned" `search_performed` snapshot. `tap.ts` (`stashHandoffForDetail`) is the pure navigation seam: it stashes the tapped result into the shared detail-handoff and returns the `/discover/detail` route string for the caller to push — by its own header comment, click tracking is deliberately the caller's concern and stays fire-and-forget. `hooks/useDiscoverLogic.ts`'s `onResultTap` is the actual telemetry site: it fires a fire-and-forget `result_clicked` event via `recordEvent.mutate(...)` alongside calling `stashHandoffForDetail`, never awaited before navigating.

Key files: `state.ts`, `hooks/useDiscoverSearch.ts`, `hooks/useDiscoverLogic.ts`, `hooks/useDebouncedSearch.ts`, `hooks/useImpressionLogger.ts`, `impressions.ts`, `tap.ts`, `ui/DiscoverScreen.tsx`.
