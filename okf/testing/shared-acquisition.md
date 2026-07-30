---
type: TestSelection
title: Test selection — shared/acquisition
description: Which of the twenty taxonomy categories apply to the mobile downloads-bar/track-status slice, which were rejected and why, and the mutation result. Done — 0% to 98.83% coverage, three defects fixed.
resource: apps/mobile/src/shared/acquisition/
tags: [testing, mobile, shared, acquisition, download]
verified_commit: 98dcd6a8b6c41a7ceeab71d24771c1752819a8eb
---

SLICE: `apps/mobile/src/shared/acquisition/`
TAXONOMY: [test-taxonomy](../playbooks/test-taxonomy.md)

Rebuilt blind on 2026-07-30, same protocol as `shared/events`: authors received the source and the taxonomy only, barred from `okf/`, git history, and any nested `CLAUDE.md` test list. The eight findings below predate the blind pass — they were produced by an agent-driven mutation audit on 2026-07-29 against a suite deleted on 2026-07-30, and live nowhere else. Each is a defect that survived a mutation and had not shipped as of this run; each is owed a Regression test asserting the intended behaviour.

**STATUS: done.** 0% → 98.83% statement / 95.49% branch coverage (per-file floor raised in `jest.config.js`, `src/shared/acquisition/**`: 97/88/100/100, raise-only). 106 tests across 7 files. Three real defects found and fixed (a NUL-byte separator bug, a banned "songs" noun, a `remove()` spread that would have wiped every other track on any single-track removal) plus several timer-interleaving and `artworkUrl`-preservation gaps closed. Stryker (scoped run): 79.44%, not yet CI-gated — see Mutation audit below.

## SELECTED

- **Table** — `stageToPhase`'s six named stages plus `null`/`undefined`/unknown-string fallback to `'working'`; `phaseLabel`/`stageLabel` composition; `PHASE_RANK` boundary comparisons in `downloadStore.progress`. `stagePhase.ts`, `downloadStore.ts`.
- **Derivation** — `DownloadsBar` computes `heading`, `activeIndex`, and `count` from `items` and the aggregate `phase`. Extracted to a pure function first per the taxonomy's done-condition, then the truth table over its outputs is covered.
- **Reducer** — `useDownloadStore` (`start`/`progress`/`complete`/`fail`/`remove`/`reset`) and `useTrackStatusStore` (`patch`/`remove`/`link`/`unlink`) — every legal transition, including the two guards named in Regression below.
- **Property** — `aggregatePhase`'s rank-fold is order-independent: for any permutation of a fixed set of `(trackId, phase)` entries, the result is unchanged. `fast-check` is already a devDependency (used in `shared/events`).
- **Cross-surface contract** — `stagePhase.ts`'s `STAGE_TO_PHASE` keys (`search`, `select`, `download`, `tag`, `store`, `update_track`) must equal every `func (s *\wStep) Name()` in `services/go-api/internal/acquisition/service/step_*.go`, derived from the Go source at test time, not restated as a literal.
- **Liveness** — `DownloadsSheet`'s `DownloadRow` subscribes directly to `useDownloadPhase(trackId)`; mutate `useDownloadStore` and assert the rendered phase label changes without a remount.
- **Idempotence / replay** — `useTrackStatusStore.remove` applied twice equals applied once (guards the Regression #2 defect below); `downloadStore.progress` delivering the same phase twice is a no-op (guards Regression #4).
- **Concurrency / ordering** — two tracks' entries updated in the same tick must not clobber each other (Regression #1); `complete()`'s two scheduled timers must be cancelled by an interleaved `fail()`/`remove()` so no stale timer fires afterward.
- **Timing / dwell** — `FINISHING_DWELL_MS`, `DONE_HOLD_MS`, `FAILED_HOLD_MS` asserted mid-window, not just at the final state (Regression #6).
- **Accessibility** — `DownloadsBar`'s `accessibilityLiveRegion`, `accessibilityRole`/`accessibilityLabel`, and `useAnnounceChange` call; `DownloadsSheet`'s scrim close button label.
- **Regression** — the eight survivors below, each as a test asserting the intended behaviour; the current pass will confirm which are already fixed by the blind rebuild versus still open.
- **Mutation audit** — see below.

## REJECTED

- **Legacy/compat** — both stores are in-memory only (no `persist` middleware); this slice loads no historical on-disk shape. Persistence for acquisition is `shared/offline`'s `pinnedStore`.
- **Persistence round-trip** — no I/O in this slice for the same reason.
- **Invalidation** — this slice performs no TanStack Query cache invalidation; that's `shared/events`' `INVALIDATION_MAP`.
- **Adversarial** — this slice's public functions (`progressDownload`, `patchTrackStatus`, …) take already-validated typed arguments from `applyServerEvent`, which owns the trust boundary. `stageToPhase`'s unknown/null fallback is exercised as a Table boundary, not a trust-boundary case.
- **Failure injection** — no I/O call site in this slice; timers only, and timers don't fail.
- **Security** — no secret, token, or credential is handled here.
- **Functional / acceptance** — no committed spec describes this UI: `docs/specs/acquire-track/spec.md` predates it and explicitly excludes "progress bars, download animations" from v1 scope. The behaviour it has is asserted via Derivation and Liveness instead.
- **Invariant / architecture** — no mechanically-checkable rule is distinct to this slice beyond what Reducer and Liveness already enforce; import direction is enforced repo-wide via eslint zones, not per-slice.

## DEFERRED

- **Device e2e** — no device/Maestro harness runs in CI for any slice yet; tracked at the programme level in `docs/specs/test-hardening/plan.md`'s Outstanding section, not reopened per-slice.

## REGRESSION CANDIDATES

Verified against the source at the time, and confirmed **not** merely scope artifacts of the audit's narrow test run:

- **`trackStatusStore.patch` wipes every other track's status.** `set((s) => ({ statuses: { ...s.statuses, [trackId]: status } }))` mutated to drop the spread survived. Two tracks acquiring concurrently would erase each other's liveness state, so one disappears from ownership tracking mid-flight. Confirmed repo-wide: the only test that touched this store patched a single fixed `'t-1'`, so no test ever had two tracks in it at once.
- **`removeTrackStatus` becomes a permanent no-op when its guard is inverted.** `if (!(trackId in s.statuses)) return s;` → `if (trackId in s.statuses) return s;` survived. Called on `track_deleted`, so a deleted track never clears its stale acquisition status; if its id is later reused by the re-acquire flow, the stale status is observable until the next patch. Confirmed repo-wide — no test imported `removeTrackStatus` at all.
- **`downloadStore.progress` lets a `done` event clobber a `failed` terminal state.** The guard `cur.phase === 'done' || cur.phase === 'failed'` mutated to drop the `'failed'` clause survived; `done` and `failed` share rank 3, so a same-rank event slips through and a failed track briefly renders as succeeded.
- **`downloadStore.progress` drops a same-phase repeat.** `PHASE_RANK[phase] < PHASE_RANK[cur.phase]` → `<=` survived. A later `downloading` tick carrying freshly-resolved artwork or title is discarded, so a track's metadata freezes at whatever was known when the phase was first entered.
- **`setPhaseIfPresent` swaps `title` and `artist`.** Both are `cur?.x ?? null` on adjacent lines; swapping them survived, so every track entering `done` or `failed` shows its artist where its title belongs in the downloads bar and sheet. No test asserted on either field at those transitions — only on `phase`.
- **The done-hold dwell is 500ms shorter than intended.** `schedule(…, FINISHING_DWELL_MS + DONE_HOLD_MS)` → `DONE_HOLD_MS` survived, because the covering test advances all timers and asserts only the final state. Timing/dwell must be asserted **mid-window**.
- **`useActiveDownloadItems`' filter can be swapped.** `.filter((e) => e.phase !== 'failed')` → `!== 'done'` survived: failed downloads would show in the dock forever and done entries vanish instantly instead of playing out their dwell. This is an **uncovered-code** survivor — the hook has no test anywhere in the repo, not merely a weak one.
- **`PHASE_LABEL.done` can read "Failed".** Mutating the copy survived; `DownloadsBar` renders `phaseLabel(phase)` as the caption under its heading, so it would read "Failed" directly beneath a heading saying "Done". Only `finding`/`downloading`/`finishing`/`working` were asserted.

Two further `trackStatusStore` survivors from the same audit — dropping `artist` from `trackIdentityKey`, and storing the identity→trackId link backwards — were **scope artifacts**, not real gaps: `features/detail/__tests__/ownership-liveness.test.ts` covered both, and that file was outside the audit's assigned run. It has since been deleted with the rest of the suite, so both behaviours need re-covering here or in the detail slice.

## MUTATION AUDIT

Pre-rebuild baseline (2026-07-29, against the deleted suite): 8 mutations applied, **0 killed**, 8 survived (the eight regression candidates above).

Post-rebuild (2026-07-30), two mechanisms as ADR-0020 requires:

**`test-assassin`, one agent per authored test file, hand-applied mutations, verified by hand (mutation → red → revert):**

| file | applied | killed (incl. repair) | left as accepted/documented |
|---|---|---|---|
| `downloadStore.ts` | 15 | 12 | 3 (2 predicted-impossible branches; 1 rank-fold tie, equivalent) |
| `trackStatusStore.ts` | 9 | 9 | 0 |
| `stagePhase.ts` | 17 | 15 | 2 (both equivalent: exhaustive-map/falsy-coercion fallbacks unreachable in practice) |
| `useActiveDownloads.ts` + `audioCacheInvalidation.ts` | 8 | 8 (1 required a repair — see below) | 0 |
| `DownloadsBar.tsx` + `DownloadsSheet.tsx` | 10 | 8 | 2 discarded as equivalent (see below) |
| **total** | **59** | **52** | **7** |

Four confirmed survivors required a repair (test strengthened, mutation re-applied by hand, confirmed red, reverted):
- `_resetAudioCacheInvalidatorsForTest` had never itself been asserted to empty the registry — added a test that registers, resets, invalidates, and asserts the stale invalidator no longer fires.
- `DownloadsBar`'s `deriveBarDisplay` never asserted `count` in the all-items-done case, so `active > 0` weakening to `active >= 0` survived — added the assertion (correct value is `items.length`, not `0`).
- `DownloadsBar`'s unmount test asserted only "doesn't throw," not that `Animated.loop(...).stop()` actually ran — rewrote it to spy on `Animated.loop` and assert `stop` fires exactly once on unmount.
- `deriveBarDisplay`'s empty-list case never asserted `phase`, so the `aggregatePhase(items) ?? 'finding'` fallback's string literal was unconstrained — added the assertion.

**Verification pass found three additional real defects (not authoring findings — genuine gaps the assassins' triage surfaced) and one confirmed source bug, all fixed:**
- **`trackIdentityKey` joined title and artist with a literal NUL byte (` `), not a space** — confirmed by byte-level read (`codePointAt` = 0 between the interpolations), not visible in any editor or diff view. Fixed at the source; the author's `it.failing` tests were promoted to passing assertions.
- **`DownloadsBar`'s pluralized heading used the banned noun "songs"** (`apps/mobile/CLAUDE.md`: *"Song" is banned — the noun is `Track`*). Fixed to "tracks"; both `deriveBarDisplay` tests asserting the literal string were updated.
- **`downloadStore.remove()`'s `{ ...s.entries }` spread was unconstrained by any two-track test** — a mutation dropping it (wiping every other track's entry) survived; added a test with two tracks where only one's dwell timer fires.
- **`complete()` and `fail()` not cancelling each other's/their own prior pending timers, and `artworkUrl` unconstrained through both transitions, were each unconstrained** — added interleaving tests (second `complete()` cancels the first's stale removal; `fail()` after `complete()` cancels the stale "done" timer so a failed track can't flip back to "done"; `reset()` cancelling real pending timers so a track restarted after reset isn't corrupted by a stale callback) and `artworkUrl`-preservation assertions alongside the existing title/artist ones.

Survivors left as accepted, with reasons (none are shipped defects):
- `downloadStore.ts:53` — a bogus entry in the private `timers` Map's array is harmless (`clearTimeout` on a non-handle no-ops) and the Map isn't exported; asserting on it would mean breaking module encapsulation for a mutation with no observable effect.
- `downloadStore.ts:158,161` — the `setPhaseIfPresent(cur undefined, create=false)` branch, confirmed unreachable: every code path that could clear an entry out from under a pending scheduled callback (`start`/`fail`/`remove`) calls `clearTimers` first, which cancels that exact callback.
- `downloadStore.ts:212` — `aggregatePhase`'s rank-fold `<` vs `<=`: ties only occur between two entries of the *same* phase, where either operator picks the same value.
- `stagePhase.ts:34`, `trackStatusStore.ts:56,61,73` — null/falsy-guard bypasses that are equivalent in practice: `STAGE_TO_PHASE`'s `?? 'working'` catches any falsy-coerced key regardless, and a plain object's `[key]` lookup for a genuinely-absent key already returns `undefined` whether or not the explicit guard runs. Distinguishing them would require a key literally equal to the string `"null"`, which no real caller produces.

**Stryker (`npm run mutate`, scoped to this slice), one CLI run before repairs and one after:**

| file | before repairs | after repairs | survived |
|---|---|---|---|
| `audioCacheInvalidation.ts` | 100.00 | 100.00 | 0 |
| `useActiveDownloads.ts` | 100.00 | 100.00 | 0 |
| `stagePhase.ts` | 81.48 | **96.30** | 1 (equivalent, see above) |
| `trackStatusStore.ts` | 94.92 | 94.92 | 3 (equivalent, see above) |
| `downloadStore.ts` | 94.33 | **96.45** | 5 (2 impossible, 1 tie, 1 encapsulation, 1 residual) |
| `DownloadsBar.tsx` | 47.19 | 56.18 | 39 |
| `DownloadsSheet.tsx` | 21.88 | 21.88 | 25 |
| **total** | **75.21** | **79.44** | **73** |

The two UI files carry nearly all of the remaining 73 survivors, and essentially all of them are `StyleSheet.create` object/string mutations and `Animated.timing` config objects (`useNativeDriver`, `duration`) that no test — in this file or anywhere in the repo — asserts on, by the project's own convention (`.claude/rules/tests.md`: *"UI components — meaningful tests on interactive logic; don't chase coverage on pure presentational components"*). `test-assassin`'s hand-triage of the same two files found exactly 2 genuine gaps (both fixed above) and discarded the rest as the same class of presentational noise; Stryker's blanket mutator set simply generates far more instances of it than a human triager would bother enumerating one by one. Chasing these to raise the number would mean writing tests that assert exact `StyleSheet` values and animation config — the coverage-inflation trap the taxonomy explicitly rejects ("Chase 100% mutation score" is a rejected alternative in ADR-0020).

**Not wired into `stryker.config.json`'s CI-enforced gate.** `stryker.config.json` supports one `mutate` glob and one global `thresholds.break` for the whole file. It currently covers only `shared/events` (committed score ≈90.75%, threshold 90, raise-only). Adding `shared/acquisition` to the same glob would average this slice's 79.44% against `shared/events`' 90.75%, landing the *combined* score below 90 and breaking the existing gate — and lowering `thresholds.break` to accommodate that is exactly what "raise-only" forbids. `stryker.config.json` was therefore left untouched (reverted after the scoped CLI run used to produce the table above), and this slice's Stryker number is recorded here only, not CI-enforced. **This is a structural gap in the ratchet design, not a decision about this slice** — every future slice with a UI/presentational component will hit the same ceiling. Surfaced in `docs/specs/test-hardening/plan.md`'s Outstanding section rather than resolved unilaterally, since fixing it means either per-slice Stryker configs/CI jobs or a different threshold strategy, both bigger than a raise-only floor bump.

## KNOWN CONTEXT

- `trackStatusStore.ts` had **no dedicated test file** before the reset, despite being the overlay behind two written rules in `apps/mobile/CLAUDE.md`: ownership is read from the server stamp overlaid with this store for liveness, and a component showing server-mutable state gets a liveness test.
- `fallow health` ranks `trackStatusStore.ts` and `downloadStore.ts` as high-impact (5 dependents each) — a change here amplifies.
- The re-acquisition flow is the app's most recently changed area and the subject of four consecutive fixes in the log, so **Legacy/compat** and **Idempotence/replay** deserve hard looks.
- `applyServerEvent.ts` drives this store from SSE and is already at 93.68% mutation score, so the event-side contract is constrained; what is missing is this store's own behaviour.
