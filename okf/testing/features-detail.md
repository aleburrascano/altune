---
type: TestSelection
title: Test selection — features/detail
description: Which taxonomy categories apply to the mobile result-detail feature slice, which were rejected or deferred and why, and the scoped mutation result. Logic units 0% → 95.26% mutation score over 401 covered mutants (382 killed / 19 survived, every survivor argued equivalent); display/gating derivation and the owned-playback handoff hardened; the .tsx detail UI and the enrichment/nav hooks deferred with reasons.
resource: apps/mobile/src/features/detail/
tags: [testing, mobile, feature, detail, derivation, gating, owned-playback, contract]
verified_commit: a0b60ff5471c3cec1411fa82b7ce3a63e3c031a4
---

SLICE: `apps/mobile/src/features/detail/`
TAXONOMY: the global workflow taxonomy (`~/.claude/workflow/taxonomy.md`), per issue #172.

Authored 2026-09-10 for issue #172 (epic #113), the third feature slice put through /qa-slice after `features/playback` (#126). `features/detail` declared `Tests: none yet` in its CLAUDE.md while carrying two live robustness domains: derivation of display/gating state on the result-detail surface, and the owned-playback handoff whose `owned-playback.ts` inlines the `'ready'` playability literal — a cross-slice invariant that must track `@shared/playback/canPlay`. Scope is the slice's testable-in-isolation units — the pure functions in `owned-playback.ts`, `play-source.ts`, `save-control-state.ts`, `save-cache.ts`, `extras.ts`, `extras-accessors.ts` and the pure helpers in `ui/helpers.ts`. The `.tsx` detail UI, the enrichment/discovery/nav hooks, and the `resolve-entity-query`/`navigation` thin seams are deferred with reasons below, not silently omitted.

**STATUS: partial-by-design.** Baseline: typecheck green, 1 suite / 8 tests (`play-source` only). After: typecheck green, **7 suites / 98 tests** (6 new files + `play-source` extended). Scoped Stryker over the seven authored source files: **95.26% mutation score (382 killed / 19 survived / 0 no-coverage; 12 runtime-error mutants excluded from the denominator)**. Restricted to the behavioural logic units (every file except the presentational style block in `ui/helpers.ts`): **382 killed / 11 survived = 97.2%**, and all eleven are argued equivalent below. The eight remaining survivors are the inert `StyleSheet.create` tokens in `ui/helpers.ts`, deliberately left dark per the taxonomy's "not a checklist to maximize over presentational primitives" and the mobile CLAUDE.md rule not to chase coverage on pure presentation. `features/detail` is **not** added to the CI Stryker gate: the committed `stryker.config.json` mutate glob is `.ts`-only shared code, and adding a UI-bearing feature slice to the gate is the `.tsx`-gate question the programme tracks separately (out of scope per #172).

## SELECTED

- **Derivation** *(live domain — display/gating)* — `saveControlState` derives the save-control lifecycle (`add`/`saving`/`ready`/`failed`) from the owned-track status by strict precedence, and `saveControlLabel`/`saveControlText` derive the announced and on-screen labels per state; `playButtonState` derives the play-all pill's label and disabled flag from the owned split. All are already extracted as pure functions. The truth table over each derived output is covered, and every branch arm asserts a value the other arm would fail (add vs failed vs saving vs ready; disabled-empty vs `Play N`-partial vs bare-`Play`-complete). `owned-playback.ts` and `save-control-state.ts` reach 100% mutation score.
- **Table** — every pure function with two or more branches gets its branches and boundaries as rows: `compactCount` (the exact `1_000` / `1_000_000` / `1_000_000_000` turns and a within-magnitude round), `formatRuntime` (the `<= 0` guard at 0 and negative, sub-hour minutes, the exact-hour `0 min` remainder, hours+minutes), `_albumYear` (release-date slice vs year fallback vs null), `extractFeaturedFromText` (parenthesised / bracketed / end-anchored / subtitle-fallthrough / no-credit), and `resolveFeatured` precedence (structured > Deezer > text, empty-list-as-absent).
- **Contract** *(live domain — the `'ready'` cross-slice invariant)* — `splitOwned` admits a track to the playable set exactly when `@shared/playback/canPlay` admits its `acquisitionStatus`. The test **derives** the expectation from `canPlay` at test time (iterating the `AcquisitionStatus` union and asserting `isPlayable === canPlay(status)`), never restating the `'ready'` literal — so if `canPlay` ever widens playability, `owned-playback.ts`'s inlined `'ready'` literal diverges and this test goes red. `toCreateTrackRequest` and `optimisticTrack` map the discovery result and request onto `CreateTrackRequest`/`TrackResponse`; fixtures cover absent/present optional fields (year, genre, track_number, album_artist, isrc, featured_artists) and the fractional-duration floor, and `optimisticTrack` is asserted to carry each populated field through — `save-cache.ts` reaches 100%.
- **Reducer / cache-shape** — `insertOptimisticTrackHome` and `replaceOptimisticTrackHome` are the `(list, track) → list` home-cache transitions: prepend-and-bump-total, undefined-passthrough, the id-already-present skip (asserted with a multi-item list so `some` is distinguishable from `every`), the in-place swap, and the dedupe-and-decrement when the real track already exists.
- **Adversarial / narrowing** — `trackExtras` and `albumExtras` narrow an untyped wire map. Fixtures cover the full map, the empty map (every field nulled), both aliases (`duration`/`duration_seconds`, `track_id`/`owned_track_id`, `acquisition_status`/`owned_acquisition_status`), an unrecognised acquisition status, empty strings (album/isrc/genre/album_artist/preview), and non-numeric/non-finite numbers (duration/year/track_position) — the boundaries where wire drift enters.
- **Functional / acceptance** — `resolvePlaySource` and `isResultPlaying` are asserted through their public surface in domain vocabulary: preview while a saved track is still acquiring, library once ready, preview kept playing across the acquisition flip, false for a paused/different/absent source, and the ready-but-no-trackId fallback to preview (the mutation-surfaced gap below).
- **Regression** — the `resolvePlaySource` mutation survivor turned up and killed: `trackId !== null` mutated to `true` survived until a `acquisition_status: 'ready'` result with no `trackId` proved the guard load-bearing (it must fall through to the preview rather than emit a `{ library, trackId: null }` source).
- **Mutation audit** — see below.

## REJECTED

- **Property** — no unit here holds a law over an unbounded generated input space; the branch spaces are small and enumerated as Table rows.
- **Legacy / compat** — the detail slice reads a live in-memory handoff and current wire shapes; it persists no versioned shape of its own and loads no historically-written data. Optional/absent-field tolerance is covered under Adversarial/narrowing.
- **Persistence round-trip · Migration & rollback** — the slice owns no disk state and changes no persisted shape. The optimistic cache entries live in the TanStack Query cache (in-memory), covered under Reducer/cache-shape.
- **Invalidation · Liveness** — the query-key invalidation and store-liveness for ownership live in `useOwnedTrack`/`useSaveTrack` (hook layer) over `@shared/acquisition/trackStatusStore` and `@shared/lib/query-keys`; the liveness *policy* is `shared`'s and is carried, not re-decided. The `.tsx` liveness test (mutate the status store, assert the rendered control changed) is DEFERRED with the UI below.
- **Idempotence / replay · Concurrency / ordering · Timing / dwell · Resource lifecycle** — no unit here replays an input, interleaves two operations, observes a duration, or acquires a released resource. `useLateralNav`'s `searchingRef` reentrancy guard is the one ordering concern and lives in the hook layer (DEFERRED).
- **Error contract · Failure injection · Observability** — the tested units perform no I/O and surface no distinguishable failure classes; network failure, provider-status gating (`useDetailEnrichments`) and the save-error surface are hook-layer/`.tsx` concerns (DEFERRED). No unit emits a diagnostic channel.
- **Security** — no secret is handled by the tested units; the bearer token is `@shared/api-client`'s concern.
- **Configuration · Load & degradation · Performance budget** — no environment-dependent behaviour, capacity limit, or latency bound in scope.
- **Accessibility** — the tappable roles/labels and ≥48pt targets are `.tsx` concerns (DEFERRED with the UI); `saveControlLabel`'s announced string is covered under Derivation.
- **Invariant / architecture** — the cross-feature import ban is enforced mechanically by `.fallowrc.json`, not by a test here; the `'ready'` playability invariant is covered under Contract by deriving from `canPlay`.
- **End-to-end** — no Maestro/device harness runs in CI for any slice (programme-level Outstanding).

## DEFERRED (with follow-up)

- **The detail `.tsx` UI** — `DetailScreen`, `DetailScaffold`, the three per-kind bodies, `TrackSaveControl`, `DiscographySections`, `RelatedTracksSection`, `LastFmEnrichmentSection`, `DetailActions`/`DetailFacts`/`Section`/`SaveGlyph`/`AlbumTrackRow`/`DetailSkeleton`. Derivation-in-a-component, Liveness (mutate `trackStatusStore`, assert the save control re-renders), and Accessibility for the UI surface. This is the `.tsx` mutation-gate question the programme parks; not resolved here.
- **The hooks** — `useSaveTrack`, `useOwnedTrack`, `useLateralNav` (the `searchingRef` reentrancy guard), `useDetailEnrichments`/`useEnrichResult` (provider-status gating, the MB-verified-vs-Deezer title rule, the title-alone-match ban), `useAlbumTracks`/`useArtistContent`, `useAlbumDetailState`/`useArtistDetailState`. Invalidation, Liveness, Error contract, Failure injection, Concurrency/ordering — integration-level, driven by `@tanstack/react-query` and the acquisition store; deferred.
- **The thin seams** — `navigation.ts` (`openDetail` writes the handoff then pushes) and `resolve-entity-query.ts` (a `queryOptions` builder). Behaviourally thin; their `queryFn` is exercised only against a live discovery API — deferred with the hooks.
- **Device e2e** — no Maestro/device harness in CI for any slice (programme-level Outstanding).

## MUTATION AUDIT

Stryker (`stryker run`) scoped to the seven authored source files via a throwaway config (`mutate` restricted to `owned-playback.ts`, `play-source.ts`, `save-control-state.ts`, `extras.ts`, `extras-accessors.ts`, `save-cache.ts`, `ui/helpers.ts`), jest runner with `enableFindRelatedTests`, `disableTypeChecks`, `coverageAnalysis: all`. Run `inPlace` because the worktree's `node_modules` is a junction the default sandbox cannot resolve `jest` through; `perTest` was replaced with `all` after it mis-scored covered mutants under `enableFindRelatedTests`. The config is not committed — the CI gate stays `.ts`-shared-only per the `.tsx`-gate question above.

| file | score | killed | survived | no-cov |
|---|---|---|---|---|
| `owned-playback.ts` | 100.00 | 50 | 0 | 0 |
| `save-control-state.ts` | 100.00 | 43 | 0 | 0 |
| `save-cache.ts` | 100.00 | 60 | 0 | 0 |
| `extras-accessors.ts` | 96.77 | 120 | 4 | 0 |
| `extras.ts` | 92.31 | 36 | 3 | 0 |
| `play-source.ts` | 89.74 | 35 | 4 | 0 |
| `ui/helpers.ts` | 82.61 | 38 | 8 | 0 |
| **all seven (covered)** | **95.26** | **382** | **19** | **0** |
| **behavioural logic units (all but the helpers style block)** | **97.2** | **382** | **11** | **0** |

(12 further mutants on `extras-accessors.ts` are `RuntimeError` — the mutation crashes the narrowing at load; Stryker excludes them from the denominator, they are not survivors.)

### Survivors — all equivalent, argued

- **`extras-accessors.ts` ×4 — the redundant left type-guard (`#23` duration, `#47` year, `#75` status, `#107` trackPosition).** Each is `ConditionalExpression` forcing the *left* operand of the `&&` to `true`: `typeof x === 'number' && Number.isFinite(x)` → `true && Number.isFinite(x)`, and `typeof status === 'string' && (status === 'ready' | 'pending' | 'failed')` → `true && (…)`. `Number.isFinite` returns true only for a real number (no coercion), and a non-string can never `===` a string literal, so the left `typeof` check is redundant for the right check's domain — no input distinguishes the mutant. Equivalent. (The *whole-condition* force-true/false and the `typeof` equality mutant are all killed.)
- **`extras.ts` ×3 — the credit regex and null-subtitle fallback (`#138`, `#141`, `#149`).** `#138` `\s*`→`\S*` (leading optional whitespace after the bracket): the pattern is unanchored, so the engine retries from the `feat` position and still matches every real credit string. `#141` `\s+`→`\s` (the space before the capture): a single space is identical, and multiple spaces leave a leading space in the capture that `.trim()` removes — indistinguishable after trim. `#149` `subtitle ?? ''` → `"Stryker was here!"`: the fallback string is only used when the subtitle is null, and any string without a feat/ft/featuring/with keyword (this one included) yields no match, exactly as `''` does. All three equivalent.
- **`play-source.ts` ×4 — non-matching injected candidates (`#250`, `#251`, `#256`, `#260`).** `#250` seeds `candidates` with a junk string; `#251`/`#256` force-push a `{ library, trackId: null }` / `{ preview, previewUrl: null }` candidate; `#260` blanks the `'preview'` kind literal. `isResultPlaying` folds candidates through `isCurrentlyPlaying`, which matches a library source only on a non-null `trackId` and a preview source only on a real `previewUrl` — a null-id, null-url, or junk candidate can never match a legitimately-playing source, and `isCurrentlyPlaying` already treats any non-`'library'` kind as preview, so the blanked literal still matches by URL. None is observable. Equivalent. (The load-bearing `!== null` guards and the object/URL literals are all killed, including the `trackId !== null` guard surfaced and killed as the Regression row.)
- **`ui/helpers.ts` ×8 — the `sharedStyles` StyleSheet block.** `ObjectLiteral`→`{}` and `StringLiteral`→`""` on `trackRow`/`trackInfo`/`retryButton`/`sectionTitle`/`albumsSection` and the `flexDirection`/`alignItems` tokens. These are inert presentational style constants with no behaviour; killing them means asserting exact StyleSheet values, which the test conventions (no snapshot/visual diffing) and the mobile CLAUDE.md ("don't chase coverage on pure presentational") both reject. Left dark deliberately. The behavioural helpers in the same file (`compactCount`, `formatRuntime`, `_albumYear`) are fully killed.

## LEFT DARK (deliberate)

The detail `.tsx` UI, the enrichment/discovery/nav hooks, the two thin seams (`navigation.ts`, `resolve-entity-query.ts`), and the `sharedStyles` presentational block are out of this pass's scope — recorded above under Deferred / argued-equivalent so a later reader can tell a reasoned deferral from a hole.
