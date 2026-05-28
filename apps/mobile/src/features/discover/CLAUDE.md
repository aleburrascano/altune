# discover — feature-local context

Mobile screen for the unified music search surface. Submit-only `TextInput` at the top, five-state body below ([state.ts](state.ts) `_viewForState` drives the switch), partial-failure banner sits as a sibling of results, empty-no-query renders the user's last-10 distinct searches via [useSearchHistory.ts](hooks/useSearchHistory.ts). Shipped under spec `docs/specs/discover-music-v1/spec.md` + ADR-0007.

## Key terms

- **DiscoverView** — five-state union: `loading | empty-no-query | results | zero-results | full-error`. Lives in [state.ts](state.ts). Mirror of the AC#20 testID set. The five states are mutually exclusive; the partial-result banner is NOT a state — it composes alongside `results` (per ADR-0007 spec §3.7).
- **`partial` flag** — server-emitted; true when any provider's `status !== 'ok'`. Gates [PartialBanner.tsx](ui/PartialBanner.tsx). NOT an error condition — `results` still renders when `partial: true`.
- **`result_signature`** — server-computed stable hash `(kind, normalized title, normalized subtitle)`. Used as the testID suffix and as the click-tracking dedup key. We never compute it client-side; we echo what the wire returns. (Confirmed via spec §"result_signature definition".)
- **Submit-only trigger** — `TextInput.onSubmitEditing` is the only path that commits a query into the query state. Tapping a history row also commits. As-you-type is the v1.1 fast-follow (locked in ADR-0007).

## Patterns specific here

- **State machine lives in `state.ts` as a pure function**, same as `library/state.ts`. `_viewForState` takes `{query, isLoading, data, error}` and returns the view literal. Tests assert the helper directly; the JSX branches in `DiscoverScreen.tsx` are trivial wrappers. Reason: same as library — jest-expo + RNTL is painful for full screens; pure helpers stay testable regardless.
- **`onSubmitEditing` commits `inputValue` to `committedQuery`** — the React Query hook keys on `committedQuery`, so changing `inputValue` mid-typing does NOT refire. Submit-only by construction.
- **Click tracking is fire-and-forget.** `useRecordClick` wraps `useMutation`; errors are swallowed in `onError`. The user never sees a click-failure toast — telemetry being best-effort is intentional per ADR-0007 §3.12.
- **History row text truncates at 40 chars** with a `…` suffix client-side. Full query is preserved in the server's `discovery_search_history.query` column; the truncation is purely visual.
- **TestIDs are load-bearing** for AC#20:
  - `discover-loading` — initial-load spinner
  - `discover-empty-no-query` + `discover-history-row-<idx>` — empty state with history rows
  - `discover-results` — results container (wraps PartialBanner + FlatList)
  - `discover-partial-banner` — sibling banner; appears only when `_shouldShowPartialBanner` returns true
  - `discover-zero-results` — 0 results returned from a non-empty query
  - `discover-full-error` + `discover-retry` — fetch failure with retry button
  - `discover-search-input` — the TextInput itself
  - `discover-row-<kind>-<position>` — individual result row
  Never rename these without updating [docs/specs/discover-music-v1/spec.md](../../../../../docs/specs/discover-music-v1/spec.md).

## Known gotchas

- **`EXPO_PUBLIC_API_URL` is baked at bundle time.** Same gotcha as `library/`. After changing `.env`, run `npx expo start --clear`. The symptom otherwise is "search hangs forever, no API logs" — the bundle is hitting the stale URL.
- **Bearer injection is unconditional.** `shared/api-client/index.ts` injects `Authorization: Bearer <supabase access token>` on every `apiFetch`. There's no opt-out here; if the user isn't authenticated, the discovery endpoint returns 401 and the screen renders `discover-full-error`. AuthGate at the route level prevents this in practice.
- **Last.fm hook fires unconditionally on mount.** [useSearchHistory.ts](hooks/useSearchHistory.ts) is a `useQuery` with no `enabled` gate — it fetches `/v1/discovery/search-history` whenever the screen mounts. Cheap (one Postgres query, <50ms) but worth knowing if you ever want lazy history.

<!-- AUTO-MAINTAINED:BEGIN -->
<!-- /update-nested-claude-md regenerates this block after every 3rd commit touching this folder.
     Do not hand-edit this block — your changes will be lost on next regeneration.
     Hand-edit above (Key terms / Patterns / Gotchas). -->

## Auto-maintained

### Files

- [state.ts](state.ts) — pure `_viewForState` + `_shouldShowPartialBanner` helpers; no RN imports so jest runs without RN transform.
- [hooks/useDiscoverSearch.ts](hooks/useDiscoverSearch.ts) — `useQuery<DiscoverySearchResponse>` keyed on trimmed query; `enabled` only when query non-empty.
- [hooks/useSearchHistory.ts](hooks/useSearchHistory.ts) — `useQuery<DiscoverySearchHistoryResponse>`; powers empty-no-query state's history list.
- [hooks/useRecordClick.ts](hooks/useRecordClick.ts) — `useMutation<void, Error, ClickPayload>`; swallows errors (best-effort telemetry).
- [ui/DiscoverScreen.tsx](ui/DiscoverScreen.tsx) — entrypoint; owns `inputValue` + `committedQuery`; switches on `_viewForState` output.
- [ui/DiscoverRow.tsx](ui/DiscoverRow.tsx) — single result row; testID `discover-row-<kind>-<position>`.
- [ui/PartialBanner.tsx](ui/PartialBanner.tsx) — warning banner above results when any provider not `ok`.

### Public API surface

- `DiscoverScreen` (default export of [ui/DiscoverScreen.tsx](ui/DiscoverScreen.tsx)) — consumed by `apps/mobile/src/app/discover.tsx` (Expo Router page).
- `_viewForState`, `_shouldShowPartialBanner` — exported for unit testing; not consumed by other features.

### Dependencies on other features / shared

- `@shared/api-client/discovery` — `searchDiscovery`, `listSearchHistory`, `recordClick` + wire types.
- `@shared/api-client/index` — `apiFetch` underlying transport (transitively).
- `@tanstack/react-query` — `useQuery` + `useMutation`, via the root `QueryClientProvider`.
- No cross-feature imports (vertical-slice rule preserved).

### Test files

- [__tests__/state.test.ts](__tests__/state.test.ts) — 7 tests on `_viewForState` (all five view-state branches) + `_shouldShowPartialBanner` (banner-on / banner-off / undefined-data).

<!-- AUTO-MAINTAINED:END -->
