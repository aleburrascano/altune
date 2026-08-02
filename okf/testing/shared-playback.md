---
type: TestSelection
title: Test selection — shared/playback
description: Which of the twenty taxonomy categories apply to the mobile Queue slice, which were rejected and why, and the mutation result. Done — 0% to 99.11% coverage, 92.78% mutation score, now CI-gated.
resource: apps/mobile/src/shared/playback/
tags: [testing, mobile, shared, playback, queue, zustand]
verified_commit: 6d8e87e5387bf7dd8106e453dabeedd539d7ae51
---

SLICE: `apps/mobile/src/shared/playback/`
TAXONOMY: [test-taxonomy](../playbooks/test-taxonomy.md)

Rebuilt blind on 2026-07-30, same protocol as `shared/events` and `shared/acquisition`: authors receive the source and the taxonomy only, barred from `okf/`, from git history, and from any nested `CLAUDE.md` test list.

**STATUS: done.** 0% → **99.11% statement / 99.26% branch / 100% function** coverage (per-glob floor in `jest.config.js`, `src/shared/playback/**`: 99/99/100/99, raise-only). **224 tests across 14 files.** Stryker: **92.78%**, and because this slice has no presentational component it clears the committed break threshold on its own — so it is the first slice added to the CI-enforced mutation gate since `shared/events`, taking the combined `mutate` glob to **91.69%** and the threshold from 90 to **91**. Two source defects fixed, one invariant repaired, five hand-mutation survivors and one Stryker survivor killed.

Its four predecessors (`canPlay.test.ts`, `previewUrl.test.ts`, `queue-generation.test.ts`, `queueStore.test.ts`) were deleted with the rest of the suite on 2026-07-30; what they encoded is harvested under Regression candidates below so it is not lost with the session.

## SELECTED

- **Table** — every pure function here has two or more branches: `canPlay` (`ready` vs the other statuses vs `null`/`undefined`), `trackKey` (`library:` vs `preview:` prefix), `isCurrentlyPlaying` (the `status × source.kind × track-identity` matrix, with `'loading'` deliberately counting as active), `getPreviewUrl` (string / empty string / non-string / absent), `buildPlayableQueue` (the `findIndex` → `Math.max(0, …)` boundary, where a target that is *not* in the playable set must fall back to index 0, not −1), `toPlaybackTrack` and `currentTrackToPlaybackTrack` (`null → null` for artwork versus `null → undefined` for duration — two different coercions on adjacent lines), and `REPEAT_CYCLE`'s three arms.
- **Reducer** — `useQueueStore` is the slice: fifteen actions over an eight-field state. Every state × action pair the domain permits gets a case, not a diagonal — in particular each action against an *empty* queue, a *single-track* queue, and a *shuffled* queue, and the cursor at first / middle / last. Illegal pairs are asserted as rejected: `skipToIndex`, `reorderQueue` and `removeFromQueue` out of range must be no-ops that return `null` or leave state identical.
- **Property** — the taxonomy names this slice's invariant by name: `playOrder` must always be a permutation of `tracks`' indices. Stated as a law and generated with `fast-check` (`^4.9.0`, already a devDependency, used in `shared/events`) over sequences of arbitrary actions: after any sequence, `playOrder` sorted equals `[0..tracks.length-1]`, `currentIndex ∈ [-1, playOrder.length-1]`, and `orderedQueueTracks` has exactly `playOrder.length` entries. Shrunk counterexamples become Table rows.
- **Cross-surface contract** — `currentTrackToPlaybackTrack` reads the server-embedded now-playing snapshot field by field; the contract is derived at test time from `currentTrackResponse`'s `json:` tags in `services/go-api/internal/playback/adapters/handler/queue_handler.go` (`id`, `title`, `artist`, `artwork_url`, `duration_seconds`, `acquisition_status`), never restated as a literal. Same for `QueueSource.kind`'s three values against `queueSourceDTO`, and for the `TrackResponse` fields `toPlaybackTrack` consumes. Go's `DurationSeconds` is `*float64` and `ArtworkURL` a `*string`, so the null variant of each is part of the contract, not an edge case.
- **Legacy / compat** — `restoreQueue` is the resume entry point, fed by a snapshot the server persisted from a possibly older client. Its clamp is cited in the taxonomy. Fixtures for: `currentIndex` past the end, negative `currentIndex`, an empty `playOrder` with non-empty `tracks`, and a `playOrder` referencing an index no longer present in `tracks` (a track deleted between save and restore).
- **Idempotence / replay** — `syncCurrentIndex` is documented as *the* idempotent reconciliation for native-driven transitions; `apply(apply(e)) === apply(e)` for every `(index, key)` pair, including the case where the native player re-reports a position the store already holds. Also `setShuffled`, `setRepeatMode`, and repeated `clearQueue` — where `generation` must still advance monotonically and never rewind.
- **Adversarial** — three trust boundaries cross into this slice. `getPreviewUrl` reads an untyped `Record<string, unknown>` straight off a discovery result's `extras`. `syncCurrentIndex` takes an index *and* a `trackKey` reported by the native player, which is precisely the drift this design exists to survive: unknown key, key present at a different position, key present twice, negative index, index past the end, key absent with index in range. `restoreQueue` takes server/persisted data (covered above under Legacy/compat).
- **Failure injection** — every native call in `useQueuePlayback` is fired as a floating `void promise` (`startQueue`, `appendToQueue`, `insertNext`, `skipNext`, `skipPrevious`, `skipToQueueIndex`, `removeQueueIndex`, `reorderUpcoming`). Each call site gets a case where the injected `PlaybackControls` rejects, asserting what the store is left holding — the store mutation has already committed by then in every case, so a rejected native call is exactly the store↔native desync the lockstep invariant forbids.
- **Concurrency / ordering** — `useQueuePlayback` pairs a store mutation with a native call at eight sites and the order is load-bearing: `moveQueueItem` and `toggleShuffle` must recompute the `upcoming` slice *after* the store mutation, `clearUpcoming` must read the store at call time and iterate descending so the playing track can never be deleted, and `playNext` must capture `insertPos` *before* mutating. Both interleavings driven and asserted.
- **Functional / acceptance** — `docs/specs/playlist-queue-polish/spec.md` §2 states queue-level product promises this slice owns, assertable through the `useQueuePlayback` facade in `docs/ubiquitous-language.md` terms with no reference to internals: the now-playing row is not draggable (a reorder never moves or restarts the playing Track), "Up Next" is exactly the Tracks after the current one in play order, tapping an upcoming Track plays that Track, and Clear never removes the playing Track. The spec's visual criteria belong to `features/playback`.
- **Invariant / architecture** — two of the four invariants in this slice's `CLAUDE.md` are mechanically checkable and get a test that fails in CI when violated: (1) `canPlay.ts` is the only file in the slice comparing an acquisition status to `'ready'`; (2) no module outside `shared/playback/` and `features/playback/` imports `useQueueStore`. See Findings — (1) does not currently hold.
- **Regression** — the five incidents the deleted suite encoded, harvested below, plus every survivor this run produces.
- **Mutation audit** — see below.

## REJECTED

- **Derivation** — the category exists to force a derivation *out* of a component; this slice contains no component (zero `.tsx` files). Its gating state — `hasNext`, `hasPrevious`, `isCurrentlyPlaying`, `canPlay` — is already pure, exported, and callable without rendering, so the extraction the done-condition demands is already done. The truth tables themselves are covered under Table and Reducer.
- **Persistence round-trip** — no disk I/O and no `persist` middleware. The Queue's durable copy is server-side (`PUT /v1/playback/queue-state`), written by `features/playback/hooks/useQueueResume.ts`; this slice only receives the restored values back through `restoreQueue`, which is covered under Legacy/compat.
- **Invalidation** — this slice touches no TanStack Query key. Queue state is Zustand-owned; cache invalidation is `shared/events`' `INVALIDATION_MAP`.
- **Liveness** — no component in this slice renders anything. `useQueuePlayback` subscribes deliberately to one *stable* action (`useQueueStore((s) => s.loadQueue)`) so the facade does not re-render on queue changes, and reads live state via `getState()` at call time instead. The screens that render Queue state — `QueueSheet`, `MiniPlayer`, `FullPlayer` — live in `features/playback` and get their liveness tests in that slice.
- **Timing / dwell** — no duration is held or measured here; there is not a single timer in the slice. `RESTART_THRESHOLD_MS` is a comparison threshold, not a dwell, and the code comparing against it is `features/playback`'s previous-button and remote handler, so its boundary belongs to that slice.
- **Security** — no secret, token, or credential passes through this slice. The audio URL's bearer token is attached in `features/playback`.
- **Accessibility** — no component, no announcement, no hit target.

## DEFERRED

- **Device e2e** — no device/Maestro harness runs in CI for any slice yet; tracked at programme level in [programme](programme.md)'s Outstanding section, not reopened per-slice. This slice is where its absence bites hardest: the store↔native 1:1 that `syncCurrentIndex` exists to repair can only genuinely break on a real `TrackPlayer`.

## REGRESSION CANDIDATES

Harvested from the four test files deleted on 2026-07-30 (`git show 76a029d^`), each encoding a behaviour someone paid to learn. They are recorded here because the transcript that surfaced them does not survive the session; the blind authors do not see this list.

- **`generation` must bump when a Queue is *replaced* and never on a transition within one.** The deleted `queue-generation.test.ts` had five cases: bumps on `loadQueue`, bumps on `restoreQueue`, does *not* bump on skip/enqueue/shuffle, and — twice over — **never rewinds** when the Queue empties or when the last Track is removed. `generation` is the ownership token a slow async resume reads before its network fetches and re-checks after, so a rewind is an ABA bug: a stale resume would conclude it still owns a Queue the user has since replaced.
- **`syncCurrentIndex` must heal native drift, not follow it.** Six deleted cases: takes the native index when no key is supplied, ignores an out-of-range index, follows the *identity* when the native queue has drifted, resolves the identity through the **play order** rather than the track order, ignores a key that is not in the Queue at all, and picks the occurrence **nearest the reported index** when the same Track appears twice. Before this existed, one dropped native slot silently offset every later transition and the UI showed the wrong Track — artwork, title and lyrics — until the Queue was rebuilt.
- **Shuffle is tail-only.** `toggleShuffle` must shuffle only the entries after `currentIndex`, leaving history and the playing Track in place, and un-shuffling must sort only that tail back to ascending order. The playing Track never moves, so the native player never re-buffers it.
- **`skipToPrevious`'s `if (prev >= 0)` guard survived a mutation in the 2026-07-29 audit** — cited in the programme as the case that refuted "refactor for testability first". Only an input at `currentIndex === 1` distinguishes `>= 0` from `> 0`; extracting the condition to a named function would not have killed it. That exact row is owed a test.
- **Repeat-one advances on manual skip.** `skipToNext` under `repeatMode === 'one'` moves to the next Track and does *not* wrap at the end; only `'all'` wraps. This mirrors native `RepeatMode.Track`, where repeat-one loops on auto-advance only. `hasNext()` returning true on the last Track under `'one'`/`'off'` previously rendered an enabled Next button that silently did nothing.

## FINDINGS (pre-authoring, verified against the source)

- **`playFromList.ts:10` violates this slice's own stated invariant.** `apps/mobile/src/shared/playback/CLAUDE.md` says `canPlay.ts` is the **only** place playability is checked; `buildPlayableQueue` inlines `t.acquisition_status === 'ready'` instead of calling `canPlay`. The Invariant/architecture test above is written to the rule as stated, so the source is fixed to satisfy it rather than the rule weakened. Six further inlinings exist *outside* the slice (`features/library/ui/TracksList.tsx`, `PlaylistDetailScreen.tsx`, `LibraryRow.tsx`, `features/detail/owned-playback.ts`, `features/playback/hooks/useQueueResume.ts` ×2); those are recorded, not fixed here, and the invariant test is scoped to the slice so it does not silently pass while they stand.
- **The facade invariant is half true as written.** `CLAUDE.md` says feature UIs call `useQueuePlayback` and never "the store or native controls directly". The *store* half holds — nothing outside `shared/playback/` and `features/playback/` imports `useQueueStore`, and that half is now gated. The *native controls* half does not: `usePlayback()` is called directly by `features/detail/ui/TrackDetailBody.tsx`, `features/library/ui/{FeaturingScreen,LibraryScreen,PlaylistDetailScreen}.tsx` and `features/discover/ui/DiscoverRow.tsx` — for now-playing status and for the preview path that deliberately bypasses the Queue.

## FINDINGS RESOLVED (authoring pass)

Six findings came back from the six blind authors. Each was verified against the source before acting; two were confirmed defects, two were not defects, two were out-of-scope drift.

- **`loadQueue` did not clamp an empty queue to `currentIndex: -1`** (unit-1) — confirmed and **fixed at the source**; the author's `it.failing` was promoted to a plain assertion. Detail in [shared-playback](../mobile/shared-playback.md). Reachable from a real play tap on an all-unplayable list, but unobservable today: `loadNativeQueue` returns before reading `startIndex` when the list is empty.
- **`playFromList.ts` inlined the `'ready'` literal** — confirmed and **fixed at the source** (it calls `canPlay` now), which is what lets the Invariant test below be written to the rule as stated rather than to the code as found.
- **`hasPrevious()` and `skipToPrevious()` disagree at the first position** (unit-2) — verified, **not a defect**. The Previous button is never disabled: `FullPlayer` uses `hasPrevious` only to choose a dim colour, `|| positionMs > RESTART_THRESHOLD_MS`, and its handler always fires — seek-to-zero past the threshold, step back under it. `hasPrevious()` answers "is there an earlier Track"; `skipToPrevious()` implements "step back, or restart if already at the start". The ADR's "enabled button that does nothing" concern applies to Next, which *is* `disabled={!hasNext}`, and there the two agree at every boundary.
- **The cross-surface contract is a subset, not an equality** (unit-5) — **not a defect**. `currentTrackResponse` carries `acquisition_status`, which the mapper deliberately does not read; asserting set equality would fail on legitimate unread metadata.
- **Every native call in `useQueuePlayback` is a floating `void promise`** (unit-4) — verified, **not a live defect, recorded as a latent contract gap**. Both shipped providers already swallow their own rejections: `trackPlayerProvider` routes every queue call through `ignoringNativeRejection` and gives `startQueue` its own handler, and `ExpoGoPlaybackProvider`'s are all `async () => {}`. So no shipped provider can reject, and adding `.catch` at eight call sites would be error handling for a scenario the boundary already prevents. What is genuinely missing is that `PlaybackControls`' type does not *state* the non-rejection contract — a future provider could break it silently. The constraint belongs in the `features/playback` slice: assert every shipped provider's controls resolve rather than reject. The facade's failure-injection tests are still worth their place, but what they constrain is commit-ordering (the store mutation lands regardless), not a reachable failure path.
- **Six files under `features/` inline the `'ready'` literal** (unit-6, matching the pre-authoring finding) — real drift, **not fixed here**: `features/library/ui/{PlaylistDetailScreen,trackMenu,LibraryRow,TracksList}`, `features/playback/hooks/useQueueResume.ts`, `features/detail/owned-playback.ts`. Each belongs to a slice that has not been rebuilt yet; the invariant test is scoped to this slice so it cannot pass silently while they stand.

## MUTATION AUDIT

Both mechanisms, as the programme requires.

**`test-assassin`, four agents grouped by source file (not by test file, so no two agents mutated the same file concurrently), every mutation hand-applied and hand-reverted:**

| target | applied | killed | survivors |
|---|---|---|---|
| `queueStore.ts` | 39 | 33 | 4 real, 2 equivalent |
| `useQueuePlayback.ts` | 31 | 30 | 0, 1 equivalent |
| primitives + Go DTOs | 34 | 30 | 1 real, 2 equivalent |
| seam + invariant scanners | 20 | 17 | 3 real (all one root cause) |

Around 124 mutations, ~110 killed outright. Deliberately not summed to a headline figure: one agent's own tally did not reconcile (four survivors claimed against a three-row survivor table), and another narrated a single mutation as killed, then survived, then hedged — it turned out to be a real gap, but only because it was re-run by hand. The number worth trusting is the verified one below.

All eight real survivors were killed and each repair verified by hand — mutation applied, intended test observed red, mutation reverted, suite observed green:

- **Four `===` boundary gaps in `queueStore.ts`**, every one the ADR's `prev >= 0` lesson recurring: `reorderQueue`'s two shift arms were never driven with `toIndex` landing *exactly on* `currentIndex` (from either direction), and `syncCurrentIndex` was never given a reported index of exactly `0` or exactly `playOrder.length`. The last is the sharpest — a native index one past the end would have been written straight into `currentIndex`, breaking the 1:1 the whole reconciliation exists to hold.
- **Three holes in the `canPlay` invariant scanner.** The gate matched only `acquisitionStatus === 'ready'`, so a negated comparison, a Yoda-style literal-on-the-left, and — most realistically — a renamed destructured binding (`const { acquisition_status: s } = t; s === 'ready'`) all walked straight through. Fixed by matching the `'ready'` literal itself anywhere in a slice file other than `canPlay.ts`, which catches all three at once without an AST. Deliberately broad: a future legitimate `'ready'` here *should* fail, because the rule says only `canPlay.ts` may express playability. The fixture test is now an `it.each` over all five spellings, so the pattern's robustness is itself constrained.
- **`useQueuePlayback` passed the caller's `startIndex` where the post-load store index was intended** — equivalent until the `loadQueue` fix above made the two diverge on an empty list, at which point nothing constrained it.

Survivors left as equivalent, with the argument in each case: `removeFromQueue`'s `i > trackIdx` remap (the removed index cannot still be present, so `>` and `>=` agree on every value that exists); `enqueue`'s `tracks.length` vs `playOrder.length` (identical under the permutation invariant); `reorderQueue`'s `fromIndex < currentIndex` vs `<=` in the second and third arms (the `===` case is already claimed by the first arm); and two type-unsafe "drop the `kind` narrowing" variants that only compile behind an `as any`, which `@typescript-eslint/no-explicit-any: 'error'` rejects in CI — the *dangerous* form of that mutation, a kind-agnostic identity extraction that produces a genuine cross-kind collision, was killed.

**One vacuity attack worth recording.** `crossSurfaceContract.test.ts` derives both sides from source text, so it fails open if a parser ever returns nothing. Renaming a Go `json:` tag, changing a nullable pointer to a value type, and starving each parser's anchor all correctly turned it red, so its guards hold. One evasion survives: the TS-side parser matches `t.field`, so a read routed through `(t as any).field` is invisible to it. Left unrepaired — it needs an `as any` to exist, which the lint gate rejects, and reading a field the *declared* type lacks is already a compile error. What this test uniquely protects against is Go-side drift under a hand-written client type, and that is proven wired.

**Stryker (`npm run mutate`), scoped to this slice:**

| file | score | survivors |
|---|---|---|
| `canPlay.ts`, `isExpoGo.ts`, `previewUrl.ts`, `toPlaybackTrack.ts`, `trackKey.ts` | 100.00 | 0 |
| `isCurrentlyPlaying.ts` | 94.59 | 2 |
| `playFromList.ts` | 92.86 | 1 |
| `queueStore.ts` | 94.35 | 19 |
| `usePlayback.ts` | 83.33 | 1 |
| `useQueuePlayback.ts` | 77.78 | 12 |
| **total** | **92.78** | **35** |

Stryker found one real weakness the hand-triage missed, now fixed: **a test named "flips the flag without touching playOrder" asserted only `playOrder`, never the flag**, so `set({ shuffled })` → `set({})` survived — and the accompanying idempotence test compared `twice.shuffled` to `once.shuffled`, which also holds when the setter does nothing. Both now assert the value. This is the clearest case in the run for keeping both mechanisms: a test whose name claims more than it asserts is invisible to a human reading the name, and to an agent that wrote it.

Of the 35 remaining, 12 are `ArrayDeclaration` mutations on `useCallback` dependency arrays in `useQueuePlayback.ts` — the React-hook analogue of the `StyleSheet` noise that dominated `shared/acquisition`'s residue, and the reason that file scores 77.78 despite zero hand-mutation survivors across 31 targeted mutations. The rest are guard-clause and boundary variants inside `shuffleTail`'s Fisher-Yates loop and `trackAt`'s invariant-protected null guard. Triaged and recorded, not chased — per the programme's rejected alternative.

**Now CI-gated.** `shared/acquisition` could not join the Stryker gate because its two presentational components dragged it to 79.44%. This slice has no component, so the combined `shared/events` + `shared/playback` glob scores **91.69%** and `thresholds.break` was raised 90 → 91. The structural ceiling in the Outstanding list is therefore specific to **UI-bearing** slices, not to adding slices as such — logic slices can keep joining the same glob.

## LEFT DARK (deliberate)

- `queueStore.ts:84` — `trackAt`'s `trackIndex == null` guard. Unreachable while `playOrder` is a valid permutation over `tracks`, which every mutating action maintains; the one way to violate it is a tampered `restoreQueue` snapshot, and that path resolves through `nearestKeyPosition` instead. The only uncovered branch in the slice.

**The `manual` field (2026-08-02) — Regression and Property rows owed.** `QueueState` gained `manual: readonly number[]` so shuffle cannot scatter a hand-queued track. Two categories are triggered and not yet written. **Regression:** play → `addToQueue` → `toggleShuffle` leaves the queued track immediately after the current one, and the same after `toggleShuffle` twice. **Property:** the existing permutation law now has a companion — after any action sequence, every index in `manual` is a valid index into `tracks`, and `manual` never contains a duplicate; `removeFromQueue` remaps it with the same decrement rule as `playOrder`, which is the arm most likely to drift. The existing `queueStore.editing` and `queueStore.property` files pass unchanged because `manual` is empty unless `enqueue`/`playNext` ran, so nothing currently exercises the new arms at all.
