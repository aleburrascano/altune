---
type: TestSelection
title: Test selection — shared/lib
description: Which of the twenty taxonomy categories apply to the mobile shared utility slice — query keys, the extras parsers, the detail handoff — which were rejected and why, and the mutation result.
resource: apps/mobile/src/shared/lib/
tags: [testing, mobile, shared, query-keys, extras, handoff]
verified_commit: 98dcd6a8b6c41a7ceeab71d24771c1752819a8eb
---

SLICE: `apps/mobile/src/shared/lib/`
TAXONOMY: [test-taxonomy](../playbooks/test-taxonomy.md)

Slice 6 of the [test-hardening programme](../../docs/specs/test-hardening/plan.md). Rebuilt blind on 2026-07-31: authors were given the source, the types it imports, and the taxonomy only — no `okf/`, no deleted tests via git, no nested `CLAUDE.md` test list.

Seven files, 4.4 KB, and the smallest slice in the programme by source size — but not by reach. Every one of the other twelve mobile slices imports something from here, and the taxonomy's own Invalidation row names this slice's `query-keys.ts` as having **zero references in any test file** while a mutation to an `INVALIDATION_MAP` entry keyed off it survived. Low line count, high blast radius: that combination is what this record exists to constrain.

212 tests across 9 files. Baseline before the rebuild: **13.46% statements / 0% branches / 20% functions / 14.89% lines** — the only covered lines were `query-keys.ts` constants incidentally evaluated by `shared/events` tests importing the module.

## SELECTED

- **Table** — six pure functions with branches: `asyncView`'s four arms, `formatDuration`'s floor/modulo/pad at `0`, `59`, `60`, `61`, `599`, `600`, `isNetworkError`'s four regex alternates plus the `instanceof` guard, `featuredArtistsFromExtras`'s per-item type guards, `withFeaturing`'s empty/absent/populated arms, `trackToDiscoveryResult`'s four conditional spreads at `null`, `undefined`, `0` and `''`. `__tests__/format.test.ts`, `__tests__/async-view.test.ts`, `__tests__/isNetworkError.test.ts`, `__tests__/featured.test.ts`, `__tests__/track-to-discovery.test.ts`.
- **Derivation** — `asyncView` is display state that *was already extracted* from a component, which is the category's own prescribed first step; what remains is its done-condition. The complete 2³ truth table is asserted, not one row per branch, because only the overlapping inputs (`loading`+`error`, `error`+`empty`, all three) constrain the **precedence order**, and Table's one-row-per-arm form does not. `__tests__/async-view.test.ts`.
- **Reducer** — `detail-handoff.ts` is a module-global `(state, operation) → state` store with four operations over three states (empty, set-with-searchId, set-without-searchId). Every state × operation pair is a case, including the ones that look uninteresting: `clear` on empty, `get` before any `set`. `__tests__/detail-handoff.test.ts`.
- **Property** — `fast-check` laws over input spaces too large to enumerate: `formatDuration` always matches `/^\d+:[0-5]\d$/` and reconstructs `minutes*60 + seconds === Math.floor(input)`; `featuredArtistsFromExtras` never returns more entries than it was given and never returns an empty `name`; every `<x>Prefix` in `query-keys.ts` is a strict prefix of the corresponding factory's output for arbitrary arguments — the invariant TanStack's partial matching depends on and the one a dropped namespace segment breaks.
- **Cross-surface contract** — `featuredArtistsFromExtras` consumes the wire shape Go's `FeaturedArtist.ToExtrasMap` produces (`docs/ubiquitous-language.md`: "Wire form in `SearchResult.Extras["featured_artists"]`"). The contract is **derived** from `services/go-api/internal/discovery/domain/featured_artist.go` at test time — the map keys are scanned out of the Go source, never restated as literals — and asserted against what the TS parser reads. This is the `shared/events` `eventContract.test.ts` pattern applied to a second seam. `__tests__/featuredContract.test.ts`.
- **Legacy / compat** — `extras` is an untyped wire map with historical shapes still in the wild: `featured_artists` absent, `null`, `[]`, entries missing `mbid`/`deezer_id` (Go's `ToExtrasMap` **omits** both when empty, it does not null them), and entries carrying a `role` key the TS type has no field for. `TrackResponse.featured_artists` is declared optional, so absent is a real arriving shape rather than a hypothetical. Each has a fixture.
- **Invalidation** — every key factory and every prefix asserted **by identity**, not by shape: exact arrays, exact ordering, exact namespace segments. This is the category the taxonomy cites this file for. Also asserted: the prefix/factory containment that makes `queryClient.invalidateQueries({ queryKey: prefix })` reach the factory's entries, and that the three namespaces (`library`, `discovery`, `playlist`/`playlists`) cannot collide. `__tests__/query-keys.test.ts`.
- **Idempotence / replay** — `setDetailHandoff` applied twice equals once; `clearDetailHandoff` on already-cleared state is a no-op rather than a throw; `trackToDiscoveryResult` and `featuredArtistsFromExtras` are both referentially stable under re-application, and `featuredArtistsFromExtras(out)` round-trips its own output.
- **Adversarial** — `featuredArtistsFromExtras(raw: unknown)` is a trust boundary: `extras` crosses from the server as `Record<string, unknown>`. Cases: non-array, `null`, array holding `null`/numbers/booleans/nested arrays, objects with wrong-typed `name`/`mbid`/`deezer_id`, `deezer_id` as a numeric string, `NaN`, a `__proto__`-carrying entry. Plus `isNetworkError` against non-`Error` throwables (string, `null`, an object with a `.message`) and `formatDuration` against negative durations. `formatDuration` against `NaN`/`±Infinity` was **rejected**, not covered: the parameter is typed `number`, JSON carries neither literal, and `trackExtras` already filters non-finite values, so four proposed `it.failing()` markers named hardening nobody had committed to and could never be promoted.
- **Concurrency / ordering** — `detail-handoff` is a shared mutable singleton read across three features and written by four call sites. Order is the whole contract: a second `setDetailHandoff` before the first consumer reads clobbers it, and `set` → `clear` → `get` must yield `null` rather than the stale result. Both interleavings are driven and asserted.
- **Functional / acceptance** — two user-visible promises live at this layer rather than in a feature: the featuring display line (`withFeaturing` produces the "Artist, Guest, Guest" secondary line rendered by six components across four features) and the `m:ss` duration every screen shows. Asserted in `docs/ubiquitous-language.md` terms — **FeaturedArtist**, **Track** — through the public function, with no reference to internals.
- **Invariant / architecture** — three written rules over this slice are mechanically checkable and now checked in CI: `shared/lib` is pure (no source file imports `react`, `react-native`, or any runtime value from `@shared/api-client` — type-only imports are permitted and are what the slice actually uses); extraction to `shared/` requires 2+ real consumers, asserted per exported module against the feature tree; and the banned noun `song` appears in no identifier or string literal in the slice.
- **Regression** — every defect this run confirmed carries a test that fails against the pre-fix source and passes after. See the defect list below.
- **Mutation audit** — see below.

## REJECTED

- **Persistence round-trip** — nothing in the slice survives process death, and that is the design rather than an omission: `detail-handoff` is deliberately in-memory, held for the duration of one navigation, and `apps/mobile/src/shared/lib/.gitkeep` constrains the whole directory to pure utilities with no I/O. There is no write to round-trip.
- **Failure injection** — the slice performs no I/O. No filesystem, no network, no keychain, no native module; the only non-pure surface is two module-level `let`s. There is no call site at which a failure can be injected.
- **Liveness** — no rendered output here. The category *does* fire on this slice's blast radius, and that obligation is recorded rather than discharged: `detail-handoff` is precisely the "module handoff" the mobile CLAUDE.md rule names as a forbidden source of server-mutable state, so `DetailScreen` owes a liveness test proving it re-reads through `useOwnedTrack` rather than rendering the handoff snapshot. That test belongs to `features/detail` (slice 9), not here.
- **Timing / dwell** — no duration is *held* anywhere in the slice. `formatDuration` renders a duration as text, which is a Table concern; the category is about a state persisting for a user-visible window, and nothing here schedules, waits, or expires.
- **Security** — no secret, token or credential is handled. The one surface worth checking before rejecting was `query-keys.ts`, since the taxonomy's done-condition names query keys as a place secrets leak: `discoveryKeys.search(query)` and `discoveryKeys.lyrics(title, artist)` do embed user input in cache keys, but that input is search text, not a credential, and no auth material reaches this slice — `shared/auth` owns the token and `shared/api-client` injects it.
- **Accessibility** — no component, no rendered output, nothing announceable.
- **Device e2e** — deferred to the spine flow, not to this slice. No flow crosses the stack here; every function is reachable in-process.

## DEFERRED

None. Every category is either satisfied at its done-condition or rejected above with the property of the code that rules it out.

## MUTATION AUDIT

Two mechanisms, as ADR-0020 requires, and they found different things.

**`test-assassin` — 65 semantic mutations, run in three sequential passes.** Never concurrently: two assassins share one working tree, so each runs the suite while the other has a mutation applied, and every *kill* becomes unattributable. Grouped by source file, one at a time.

| pass | target | applied | killed | survived |
|---|---|---|---|---|
| 1 | `query-keys.ts` | 15 | 15 | 0 |
| 2 | `featured.ts`, `track-to-discovery.ts` | 24 | 22 | 1 (+1 equivalent) |
| 3 | `detail-handoff.ts`, `async-view.ts`, `format.ts`, `isNetworkError.ts` | 26 | 26 | 0 |
| | **total** | **65** | **63** | **1 real, 1 equivalent** |

The one survivor was `image_url: track.artwork_url ?? null` → `|| null`, and it was a real defect rather than a weak test — see the concept doc. Fixed, and the fix proven by hand: revert it, watch the new test go red, restore.

**The query-keys pass is the finding that justifies this slice's place in the queue.** All 15 died, but only 4 were caught by the pre-existing `shared/events` suites — **11 of 15 were caught only by the new file**. Those suites execute `query-keys.ts` on every run and constrain almost nothing about it, because both sides of their assertions read the *same imported constant*: `expect(invalidatedKeys(spy)).toEqual([libraryKeys.tracksPrefix, …])` still passes when `tracksPrefix` is mutated, and `setQueryData(libraryKeys.tracks('q','s'), …)` followed by `getQueryData(libraryKeys.tracks('q','s'))` still round-trips when the factory silently drops its `sort` argument. Write and read agree with each other, just not with the key a production hook computes. Among the 11: `albums` dropping `sort` (two sort orders collapse onto one cache entry, so switching Recent/A-Z never refetches), `lyrics` dropping `artist` (two artists' versions of one title share a lyrics entry), `featuring` dropping `identity` (every artist's "appears on" list collapses onto one), `suggest` cross-wired into the `search` namespace, `summary` made a child of `tracksPrefix` (the SSE patch layer would then treat a flat `ListTracksResponse` as `InfiniteData`), and `albumsPrefix` aliased to `artistsPrefix` (a track add refreshes Artists while Albums stays stale).

**Stryker — 113 mutants over the slice's 7 files, `97.35%`.**

| file | score | killed | survived |
|---|---|---|---|
| `async-view.ts` | 100.00 | 14 | 0 |
| `detail-handoff.ts` | 100.00 | 5 | 0 |
| `format.ts` | 100.00 | 6 | 0 |
| `isNetworkError.ts` | 100.00 | 4 | 0 |
| `query-keys.ts` | 100.00 | 12 | 0 |
| `track-to-discovery.ts` | 100.00 | 28 | 0 |
| `featured.ts` | 93.18 | 41 | 3 |
| **total** | **97.35** | **110** | **3** |

**All three survivors are equivalent, and each was checked rather than assumed** — the first read of the report suggested three real gaps, and re-running Stryker scoped to the file to see its *actual* replacements showed all three are half-condition drops, not the whole-condition drops they looked like:

- `!featured || featured.length === 0` → `!featured || false`. `[base, ...[]].join(', ')` is already `base` — spreading an empty array contributes no element and `join` inserts no separator for a single-element array. No input distinguishes them.
- `item !== null && typeof item === 'object'` → `item !== null && true`. Every non-object that now enters the branch (`5`, `true`, `['SZA']`, a function) reads `rec['name']` as `undefined`, fails the `name` type guard, and hits `continue`. The downstream guard absorbs it entirely.
- `typeof rec['deezer_id'] === 'number' && Number.isFinite(…)` → `true && Number.isFinite(…)`. `Number.isFinite` does not coerce: it returns `false` for `'123'`, `null`, `undefined` and `true`, so it strictly implies the `typeof` check at runtime. The `typeof` half is load-bearing for **TypeScript's narrowing**, not for behaviour — `Number.isFinite` does not narrow `unknown` to `number`. Equivalent at runtime, required at compile time.

A lesson worth keeping: the first hand-check of these three applied *stronger* mutations than the ones Stryker actually ran (`if (false)` rather than `if (!featured || false)`) and killed all three, which would have been a false all-clear in the other direction. Read the replacement the tool actually applied before ruling on it.

`shared/lib` joined the `stryker.config.json` `mutate` glob on the same terms as the three logic slices before it — it has no `.tsx`, so the glob's `.ts`-only shape excluded nothing and it again did not force the open question about component-heavy slices. Combined score **93.43 → 93.72**; `thresholds.break` stays at **93**, because 94 does not clear.

Coverage: **13.46/0/20/14.89 → 100/100/100/100**, per file and in aggregate — the second slice after `shared/api-client` where the per-file minima and the aggregate coincide, so the ratchet records what every file actually holds rather than an average hiding a floor breach.
