---
type: TestSelection
title: Test selection — features/discover
description: Which taxonomy categories apply to the mobile discover feature slice, which were rejected or deferred and why, and the scoped mutation result. Logic units went from a lone banned-noun guard to 99.21% mutation score over 126 covered mutants; derivation of the five-state view, the debounce/explicit-submit machine and the once-per-search impression guard are hardened; the react-query-bound hooks and the .tsx UI are deferred with follow-up reasons. The 'Songs' banned-noun leak was already resolved on main and its slice-invariants guard is re-proven red-before-green here.
resource: apps/mobile/src/features/discover/
tags: [testing, mobile, feature, discover, derivation, debounce, idempotence, invariant, banned-noun]
verified_commit: a0b60ff5471c3cec1411fa82b7ce3a63e3c031a4
---

SLICE: `apps/mobile/src/features/discover/`
TAXONOMY: the global workflow taxonomy (`~/.claude/workflow/taxonomy.md`), per issue #174 (epic #113, follow-up from #126).

Authored 2026-09-10 for issue #174. The slice carried a single test — the `slice-invariants.test.ts` banned-noun guard added by `e29faf60` — and no constraint on its logic units. Scope is the slice's testable-in-isolation units: the pure view/label/key functions, the impression-row projection, the navigation seam, the preserved-search module state, and the two feature-local hooks whose behaviour is a state machine (`useDebouncedSearch`) or a replay guard (`useImpressionLogger`). The react-query-bound hooks and the `.tsx` UI are deferred with reasons and follow-up tickets, not silently omitted.

**STATUS: partial-by-design.** Baseline: typecheck green for the slice (one pre-existing `library` error unrelated to discover — `FeaturingScreen.tsx:30`), 1 suite / 4 tests. After: 7 suites / **63 tests** (6 new files). Scoped Stryker over the ten mutated source files: **44.48% of total mutants killed** (125 killed / 1 survived / 155 no-coverage). Restricted to the fully-authored logic units (`state.ts`, `impressions.ts`, `tap.ts`, `search-state.ts`, `hooks/useDebouncedSearch.ts`, `hooks/useImpressionLogger.ts`): **125 killed / 1 survived = 99.21%**, and the one survivor is argued equivalent below. `features/discover` is **not** added to the CI Stryker gate: the committed `stryker.config.json` mutate glob is shared-only and this slice's residual work is in `.tsx` UI and the react-query hooks — the unresolved `.tsx`-gate question the programme parks separately (out of scope for #174).

## SELECTED

- **Derivation** — `_viewForState` maps the hook state to the five-state union (`empty-no-query` > `loading` > `full-error` > `zero-results` > `results`) via `asyncView`. Already a pure function. The truth table over the derived outputs is covered, and every precedence test asserts the arm that wins disagrees with the arm it suppresses: a blank query short-circuits even while loading; `loading` wins over `full-error` when both hold and no data has arrived; existing data suppresses both `loading` and `full-error` (background refetch/error still shows `results`); an error over an empty data set yields `zero-results`, not `full-error`; and a live query with no data, error or loading falls through to `results` (the `isError` guard's `state.data === undefined` conjunct — the mutation that dropped `state.error != null` is killed by this row).
- **Table** — every pure function with two or more branches gets its branches and boundaries as rows: `kindLabel` (singular/plural for each of artist/album/track, `plural: false` vs `true` vs absent), and `resultKey` (source-with-id path, the `external_id || fallback` boundary at an empty id, and the no-source path taking the `'x'` provider placeholder and the `title-index` fallback).
- **Reducer** *(the debounce/explicit-submit state machine)* — `useDebouncedSearch` is a `(state, event) → state` machine over `inputValue`/`committedQuery`/`isExplicitSubmit`. Every transition is driven: a debounced `onChangeText` commits only after the window and marks the commit non-explicit; `onSubmit` and `setQuery` commit immediately and mark explicit; emptying the input clears immediately; `onClear` resets to the empty non-explicit state; and the initial `isExplicitSubmit` is asserted `false`. The arms disagree — this is the source of the slice's "only explicit submits pass `save_history=true`" invariant, wired through `useDiscoverSearch`.
- **Timing / dwell** *(the debounce duration is observable)* — the 300ms window is asserted mid-window: at 299ms the committed query is still unchanged, at 300ms it updates. The trimmed value is asserted committed, so the `text.trim()` mutation is killed.
- **Concurrency / ordering** *(debounce cancellation)* — a superseded keystroke is proven cancelled: after typing, waiting part of the window, then typing again, the stale query is asserted to never commit at the point its timer would have fired, and only the latest query commits after the new window. This kills the `clearDebounce` no-op and the `if (debounceRef.current)` → `false` mutations.
- **Idempotence / replay** *(the once-per-search impression guard)* — `useImpressionLogger` emits `results_shown` at most once per `search_id`: a repeated viewable event for the same search does not re-emit (`apply` twice equals once), and a new `search_id` re-emits (the arms disagree). The `emittedFor` guard is load-bearing.
- **Functional / acceptance** — `stashHandoffForDetail` is asserted through its public effect: it returns `'/discover/detail'` and stashes the tapped result and search id so the detail screen reads them back (via `getDetailHandoff`/`getDetailHandoffSearchId`), storing `null` when no search id is supplied. `useImpressionLogger` is asserted to emit the exact analytics payload (type, `query_norm`, `search_id`, and the projected rows), and the `itemVisiblePercentThreshold` of 50 is asserted.
- **Liveness / round-trip** — `search-state` preserves the last query across a detail→back round trip: a write reads back exactly, the latest write wins over a stale snapshot, the two fields stay independent, and — via `jest.isolateModules` — both fields default to empty strings before anything is written (killing the module-init string-literal mutants). `useDebouncedSearch` is asserted to rehydrate from this saved state on mount.
- **Invariant / architecture** *(live domain — the banned noun)* — the pre-existing `slice-invariants.test.ts` mechanically forbids the noun `song`/`songs` (case- and case-style-insensitive) anywhere in the slice's `.ts`/`.tsx` sources; it is the guard that would have caught the original `KIND_LABELS.track = ['Song', 'Songs']` leak fixed in `e29faf60`. Re-proven red here — see Regression.
- **Regression** — the banned-noun leak (`e29faf60`) and the six mutation survivors this pass turned up and killed: `state.ts`'s `isError` guard `state.error != null` conjunct, `search-state.ts`'s two module-init defaults, `useDebouncedSearch.ts`'s `clearDebounce` body / `if (debounceRef.current)` guard / `text.trim()` / the `>= minChars` boundary, and `useImpressionLogger.ts`'s `viewabilityConfig` object.
- **Mutation audit** — see below.

## REJECTED

- **Property** — no unit here holds a law over an input space too large to enumerate; the pure functions are finite branch tables, covered under Table/Derivation.
- **Contract** — no unit here restates a producer's wire shape as a literal. `_viewForState`, `buildImpressionRows`, `resultKey` and `useImpressionLogger` consume the already-parsed `DiscoverySearchResponse`/`DiscoveryResult` domain types; the discovery wire contract is `shared/api-client`'s (`discovery.test.ts`, `parse`), asserted there, not restated here. The absent/null-field boundaries (`result_signature` absent → `''`, `sources[0]` absent → `null`/`'x'`) are covered as Table rows.
- **Persistence round-trip** — `search-state` is in-memory module state that does not survive process death; it is covered under Liveness. The search history that *is* server-persisted is written and read by `shared/api-client/discovery` and the react-query hooks (deferred), not by the tested units.
- **Legacy / compat** — no versioned persisted shape is consumed here; the optional-field handling is a Table concern, per the taxonomy's "folded into Contract" note.
- **Error contract** — the slice owns no distinguishable failure classes: `searchError` is collapsed to a single boolean into `_viewForState` (`error != null` → `full-error`), asserted under Derivation. The `ApiError`/`NetworkError` taxonomy is `shared/api-client`'s.
- **Adversarial · Failure injection · Security** — the trust boundary (the network) and its I/O are crossed in `shared/api-client/discovery` (parse/validate) and the react-query hooks; the tested units consume parsed domain values and perform no I/O and handle no secrets. The click/impression telemetry is fire-and-forget through `shared/telemetry` (its own `onError`), carried, not re-decided here.
- **Invalidation** — the `discoveryKeys.history` invalidation and the `setQueryData(history, {items:[]})` optimistic clear live in `useDiscoverLogic` (deferred, react-query-bound); no tested unit touches a TanStack Query key.
- **Resource lifecycle · Migration & rollback · Configuration · Load & degradation · Performance budget · Observability** — no acquired resource, persisted-shape change, environment-dependent behaviour, capacity limit, latency bound, or separate diagnostic channel in scope.
- **Accessibility** — the interactive surface is the `.tsx` UI (deferred).

## DEFERRED (with follow-up)

- **The react-query-bound hooks** — `useDiscoverLogic` (110 no-coverage mutants: the orchestration of committed query, filter reset, click/impression telemetry, history invalidation and optimistic clear), `useDiscoverSearch` (30: infinite-query page flattening, `getNextPageParam`, the `save_history` only-on-first-page rule, in-flight cancellation), `useAutocompleteSuggestions` (11) and `useSearchHistory` (4). These are Invalidation, Failure injection and Contract-of-the-hook, driven by a real `QueryClient` and the fetch double; integration-level, deferred. They are the whole of the 155 no-coverage mutants.
- **The discover UI** (`DiscoverScreen`, `DiscoverBody`, `DiscoverRow`, `ResultsList`, `BlendedSection`, `FilteredResults`, `TopResultCard`, `SuggestionsList`, `CorrectionBanner`) — Derivation-in-a-component, Liveness, and Accessibility for the `.tsx` surface, including the load-bearing testIDs (AC#20). This is the `.tsx` mutation-gate question the programme parks separately; not resolved here.
- **Device e2e** — no Maestro/device harness runs in CI for any slice (programme-level Outstanding); the debounce↔query↔navigation spine can only truly break end to end on a device.

## MUTATION AUDIT

Stryker (`npx stryker run`), scoped to the ten mutated source files (`src/features/discover/**/*.ts`, excluding `__tests__`), jest runner with `enableFindRelatedTests` + `perTest` coverage, `disableTypeChecks`. Sandbox kept outside `rootDir` (`tempDirName: ../../.stryker-tmp`) per the slice invariant. Run via a scoped config (not committed; the CI gate glob stays shared-only).

| file | score (covered) | killed | survived | no-cov |
|---|---|---|---|---|
| `state.ts` | 100.00 | 61 | 0 | 0 |
| `impressions.ts` | 100.00 | 7 | 0 | 0 |
| `tap.ts` | 100.00 | 2 | 0 | 0 |
| `search-state.ts` | 100.00 | 5 | 0 | 0 |
| `hooks/useImpressionLogger.ts` | 100.00 | 20 | 0 | 0 |
| `hooks/useDebouncedSearch.ts` | 96.77 | 30 | 1 | 0 |
| `hooks/useDiscoverLogic.ts` | — | 0 | 0 | 110 |
| `hooks/useDiscoverSearch.ts` | — | 0 | 0 | 30 |
| `hooks/useAutocompleteSuggestions.ts` | — | 0 | 0 | 11 |
| `hooks/useSearchHistory.ts` | — | 0 | 0 | 4 |
| **logic units (the six authored files)** | **99.21** | **125** | **1** | **0** |
| **all ten** | **44.48 total / 99.21 covered** | **125** | **1** | **155** |

### Survivor on the logic units — equivalent, argued

- **`useDebouncedSearch.ts:32` `if (debounceRef.current)` → `if (true)`.** The guard exists so `clearDebounce` only calls `clearTimeout` when a timer is pending. Forcing it `true` runs `clearTimeout(debounceRef.current)` when `debounceRef.current` is `null` — a documented no-op — and then re-assigns `debounceRef.current = null`, which is already `null`. No input distinguishes the two: with a timer pending both clear it; with none pending both leave the ref `null` and cancel nothing. Equivalent (defensive guard).

### No-coverage on the react-query hooks — deferred domain

155 no-coverage mutants sit entirely in the four react-query-bound hooks listed under Deferred. They belong to that follow-up work item, not to a claim made and unmet here.

## THE BANNED-NOUN LEAK (Done-when #3)

The `KIND_LABELS.track = ['Song', 'Songs']` leak — 'Songs' shipped to users as filter chips and section headings — was already resolved on `main` by `e29faf60` (`track: ['Track', 'Tracks']`), which also added the `slice-invariants.test.ts` guard. Resolution stays in-source and slice-local (the ticket's first option), not a repo-wide lift, because the out-of-scope rule confines this ticket to `features/discover` and every sibling slice already carries its own `slice-invariants.test.ts`. The only residual `song` tokens in the tree are deliberate test fixtures (`'Song Title'` in `shared/events`/`shared/acquisition` test data) and the invariant scanners' own self-tests — no production source leaks the noun.

**Red proof.** Temporarily restoring `track: ['Song', 'Songs']` in `state.ts` turned `slice-invariants.test.ts` red — `offenders` became `["state.ts"]` against the expected `[]` — and the new `kindLabel` assertion red — `kindLabel('track')` returned `'Song'` against `not /song/i`. The line was restored with a targeted edit and the suite returned to green. The guard is a genuine proving test, not a tautology.

## LEFT DARK (deliberate)

The slice's `.tsx` UI and its four react-query-bound hooks are out of this pass's scope — recorded above under Deferred with follow-up reasons, so a later reader can tell a reasoned deferral from a hole.
