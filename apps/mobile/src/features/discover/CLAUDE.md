# discover — feature-local router

Mobile screen for the unified music search surface: greeting + "Discover" title above a debounced `TextInput`, five-state body below. Sectioned Spotify-style results — filter chips (`All · Albums · Songs · Artists`), a Top Result card, then per-kind sections. Specs: `docs/specs/discover-music-v1/`, `-v2`, `-v4`; ADR-0007, restyled per ADR-0009.

`DiscoverView` is a five-state union — `loading | empty-no-query | results | zero-results | full-error` — mutually exclusive, driven by `_viewForState` in [state.ts](state.ts).

## Rules

- Keep the state machine in `state.ts` as a pure function; JSX branches stay trivial wrappers.
- Import query keys from the `discoveryKeys` factory in `@shared/lib/query-keys` — never write a `['discovery', …]` literal.
- Never compute `result_signature` client-side; echo what the wire returns.
- Never persist history on a debounced query — only explicit submits pass `save_history=true`.
- Never surface the `partial` flag; it is wire-only since ADR-0009.
- Never await the click-tracking mutation before navigating.
- Report `position` as the result's global index in `results[]`, not the section-local display index.
- Keep results surfaces on the shared `ui/ResultsList.tsx` shell; sections supply only data/key/renderItem.
- Every tappable element needs `accessibilityRole="button"` + `accessibilityLabel`; "See all" targets ≥44pt.
- Never rename a load-bearing testID without updating `docs/specs/discover-music-v1/spec.md`.
- After changing `.env`, run `npx expo start --clear` — `EXPO_PUBLIC_API_URL` is baked at bundle time.

Load-bearing testIDs (AC#20): `discover-loading`, `discover-empty-no-query`, `discover-history-row-<idx>`, `discover-results`, `discover-zero-results`, `discover-full-error`, `discover-retry`, `discover-search-input`, `discover-row-<kind>-<position>`, `discover-top-result`, `discover-see-all-<kind>`.

Why each rule exists: `okf/mobile/discover-feature.md` — read before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).

<!-- AUTO-MAINTAINED:BEGIN -->
<!-- /update-nested-claude-md regenerates this block after every 3rd commit touching this folder.
     Do not hand-edit this block — your changes will be lost on next regeneration.
     Hand-edit above (Rules / testIDs). -->

## Auto-maintained

### Files

- [state.ts](state.ts) — pure `_viewForState` + blended-view helpers (`_groupByKind`, `_topResult`, `_sectionOrder`, `_cap`); no RN imports so jest runs without RN transform.
- [tap.ts](tap.ts) — `stashHandoffForDetail`; the navigation seam, unit-testable without rendering.
- [search-state.ts](search-state.ts) — feature-local last query/input, so detail→back preserves the search.
- [hooks/useDiscoverSearch.ts](hooks/useDiscoverSearch.ts) — `useQuery<DiscoverySearchResponse>` keyed on trimmed query; `enabled` only when query non-empty.
- [hooks/useSearchHistory.ts](hooks/useSearchHistory.ts) — `useQuery<DiscoverySearchHistoryResponse>`; powers empty-no-query state's history list.
- [ui/DiscoverScreen.tsx](ui/DiscoverScreen.tsx) — entrypoint; owns `inputValue` + `committedQuery`; switches on `_viewForState` output.
- [ui/DiscoverRow.tsx](ui/DiscoverRow.tsx) — single result row; testID `discover-row-<kind>-<position>`.
- [ui/ResultsList.tsx](ui/ResultsList.tsx) — FlatList shell shared by `BlendedSection` and `FilteredResults`.

### Public API surface

- `DiscoverScreen` (default export of [ui/DiscoverScreen.tsx](ui/DiscoverScreen.tsx)) — consumed by `apps/mobile/src/app/(tabs)/discover/index.tsx`.
- `_viewForState` + blended-view helpers — exported for unit testing; not consumed by other features.

### Dependencies on other features / shared

- `@shared/api-client/discovery` — `searchDiscovery`, `listSearchHistory`, `clearSearchHistory`, `recordEvent` + wire types.
- `@shared/telemetry/useRecordEvent` — shared fire-and-forget behavioral-event hook (`result_clicked`).
- `@shared/lib/query-keys` — the `discoveryKeys` factory.
- `@shared/lib/detail-handoff` — the discover↔detail seam.
- `@tanstack/react-query` — `useQuery` + `useMutation`, via the root `QueryClientProvider`.
- `@shared/ui` — design-system primitives (ADR-0008 / ADR-0009).
- No cross-feature imports (vertical-slice rule preserved).

### Test files

- [__tests__/state.test.ts](__tests__/state.test.ts) — `_viewForState` (all five view-state branches) + blended-view helpers.
- [__tests__/tap.test.ts](__tests__/tap.test.ts) — `stashHandoffForDetail`.

<!-- AUTO-MAINTAINED:END -->
