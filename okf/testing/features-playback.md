---
type: TestSelection
title: Test selection — features/playback
description: Which taxonomy categories apply to the mobile playback feature slice, which were rejected or deferred and why, and the scoped mutation result. Logic units 0% → 97.31% mutation score over 186 covered mutants; native-slot swap/repair hardened; disk-cache prefetch/eviction and the .tsx UI deferred with follow-up tickets.
resource: apps/mobile/src/features/playback/
tags: [testing, mobile, feature, playback, native-queue, concurrency, idempotence, lifecycle]
verified_commit: a0b60ff5471c3cec1411fa82b7ce3a63e3c031a4
---

SLICE: `apps/mobile/src/features/playback/`
TAXONOMY: the global workflow taxonomy (`~/.claude/workflow/taxonomy.md`), per issue #126.

Authored 2026-09-10 for issue #126 (epic #113). First feature slice with no prior suite to declare `Tests: none yet`. Scope is the slice's testable-in-isolation units — the pure functions and the module-level native-queue coordination primitives named as live domains in `apps/mobile/src/features/playback/CLAUDE.md`. The `.tsx` player UI and the on-disk prefetch cache are deferred with reasons and follow-up tickets, not silently omitted.

**STATUS: partial-by-design.** Baseline: typecheck green, 2 suites / 6 tests. After: typecheck green, **12 suites / 84 tests** (10 new files). Scoped Stryker over the eleven mutated source files: **91.29% of covered mutants killed** (220 killed / 21 survived / 114 no-coverage; 61.97% raw total). Restricted to the fully-authored logic units (every file except `audioPrefetch.ts`): **181 killed / 5 survived = 97.31%**, and all five survivors are argued equivalent below. `features/playback` is **not** added to the CI Stryker gate: the committed `stryker.config.json` mutate glob is `.ts`-only and this slice's residual work is in `.tsx` UI and the disk cache, which is the unresolved `.tsx`-gate question the programme parks on slice 9.

## SELECTED

- **Derivation** — `derivePlaybackState` computes the player's display status (`idle`/`error`/`loading`/`ended`/`playing`/`paused`) by strict precedence, and `_lyricsView` derives the lyrics-sheet view (`loading`>`error`>`synced`>`plain`>`unavailable`). Both are already extracted as pure functions. The truth table over the derived outputs is covered, and every precedence test asserts the arm that wins *disagrees* with the arm it suppresses (error wins over buffering/ended/playing; buffering over ended; ended pins `positionMs` to `durationMs`).
- **Table** — every pure function with two or more branches gets its branches and boundaries as rows: `activeLineIndex` (the exact `> positionMs` turn, a line active at its own timestamp, `-1` before the first), `listenThresholdMs` (the `Math.min` cap where half-duration meets the flat threshold at 60_000, and the `durationMs > 0` guard at 0 and negative), `hasCrossedListenThreshold` (just-below vs exactly-at), `trackKey` (library `lib:` vs preview `prev:` prefix), the three `resumeQueue` functions, `toNativeTrack` (id/artwork/url selection), `rateLabel`, and `minutesRemaining`.
- **Concurrency / ordering** *(live domain)* — `withNativeQueue` serialises every native-queue mutation. Both interleavings are driven: the second op is proven held until the first settles, and a *rejecting* op is proven not to break the chain — the op queued behind a rejection still runs and still runs *after* it. `shouldApplyActiveIndex` is the ordering guard: while a load targeting a non-zero index is in flight it rejects the priming index-0 event the native `add` fires first, and applies the real target — the guard has tests that fail without it.
- **Idempotence / replay** *(live domain)* — `shouldApplyActiveIndex` is the feature-side replay guard for native-driven transitions: repeated priming index-0 events stay ignored (`apply` twice equals once), the guard clears exactly once when the real target arrives, and a load targeting index 0 applies its index-0 event because priming *to the first track* is the real target, not a transient. The store-side `syncCurrentIndex` idempotence is `shared/playback`'s (hardened slice 3) and is **carried, not re-decided** here.
- **Resource lifecycle** *(live domain — native slots covered, disk cache deferred)* — `swapUpcomingToLocal`/`refillSlot`: the native slot is removed then refilled on every path; the local re-add's failure falls through to a streaming re-add, and *only* that streaming re-add surfaces a `PlaybackError` (the slice's stated invariant). A slot sitting at index 0 with no active track yet is still swapped. `repairActiveToStreaming` (pre-existing `audioPrefetch.repair.test.ts`) surfaces a `PlaybackError` on a native load/play failure and stays clean on success. The on-disk prefetch cache lifecycle is DEFERRED — see below.
- **Error contract** — `playbackErrorStore` is keyed by `trackKey`; `report`/`clear` transitions are asserted on observable state, and `usePlaybackErrorFor` returns the message only for the matching key, `null` for a different track and for a null query.
- **Legacy / compat** — `resolveResumeStartIndex` and `reconstructPlayOrder` rebuild playback from a server-persisted snapshot a possibly-older client wrote. Fixtures for: saved index past the end, negative saved index, the saved track deleted between save and restore (falls back to a clamp), the saved track now first in the valid set (index 0), no id ever saved (clamp to the saved index, not to the first track), and play ids no longer present in the natural order (dropped).
- **Failure injection** — the native re-add failure paths: local add rejects → streaming re-add; both reject → `PlaybackError`; and `repairActiveToStreaming`'s load/play rejection.
- **Regression** — the two mutation survivors this pass turned up and killed: `resolveResumeStartIndex`'s `found >= 0` boundary at `found === 0` (a saved track now first in the valid set), and `upcomingSlotOf`'s `slot < 0` boundary at `slot === 0` (an upcoming slot with no active track).
- **Mutation audit** — see below.

## REJECTED

- **Reducer** — this slice owns no `(state, event) → state` machine. The Queue reducer is `shared/playback`'s `useQueueStore` (hardened slice 3); the three stores here (`playbackErrorStore`, `playbackRateStore`, `sleepTimerStore`) are flat setters, covered under Error contract and Table.
- **Property** — the permutation invariant that names an unbounded input space is `shared/playback`'s `playOrder`; nothing in this slice's pure functions holds a law over generated input.
- **Contract** — no unit here restates a producer's wire shape. `toNativeTrack` consumes the already-parsed `PlaybackTrack` domain type, and the audio stream URL is `shared/api-client`'s contract (slice 5) — asserted here only as delegation (`native.url === audioStreamUrl(id)`), not restated.
- **Persistence round-trip** — no disk state is owned by the tested units. The resume snapshot is server-persisted (written by `useQueueResume`); this slice only reads it back through `resolveResumeStartIndex`, covered under Legacy/compat.
- **Invalidation** — this slice touches no TanStack Query key; cache invalidation is `shared/events`.
- **Timing / dwell** — the only observable duration is the sleep timer, exercised by the pre-existing `SleepTimerBridge` test under an injected clock.
- **Adversarial** — the native-report boundary (index + key) is survived by `shared/playback`'s `syncCurrentIndex`; the feature-side guard against its one hostile shape (the priming index-0 event) is covered under Concurrency/ordering.
- **Security** — no secret is handled by the tested units. The bearer token flows through `toNativeTrack`'s `headers` passthrough and is asserted attached to a library stream and *dropped* when serving an explicit local stream URL; it is never logged, keyed, or persisted here.
- **Configuration · Load & degradation · Performance budget** — no environment-dependent behaviour, capacity limit, or latency bound in scope.
- **Observability** — the failure paths surface a user-facing `PlaybackError` (asserted); these units emit no separate diagnostic channel.
- **Migration & rollback** — no persisted shape changes in this slice.

## DEFERRED (with follow-up)

- **The player UI** (`FullPlayer`, `MiniPlayer`, `QueueSheet`, `LyricsSheet`, `Scrubber`, `PlayerOptionsSheets`, and `SleepTimerBridge` beyond its existing bridge test) — Derivation-in-a-component, Liveness, and Accessibility for the `.tsx` surface. This is the `.tsx` mutation-gate question the programme parks on slice 9; not resolved here.
- **The on-disk prefetch cache** in `audioPrefetch.ts` — `prefetchNext`, `evict`, `findCached`, `extFromUrl`, `presignedUrlOrNull`, and the `File.downloadFileAsync` round-trip. Resource lifecycle + Failure injection for the disk cache; needs the `expo-file-system` double's download/list failure modes. This is the bulk of the 114 no-coverage plus most of the covered `audioPrefetch` survivors.
- **The event-wiring hooks and providers** (`service.ts`, `usePlaybackSignals`, `useQueueResume`, `useLyrics`, `PlaybackProvider`/`trackPlayerProvider`/`expoGoPlaybackProvider`, `loadNativeTrack`, `seekControls`, `initPlayer`, `registerPlaybackService`) — integration-level, driven by real `TrackPlayer` events; deferred.
- **Device e2e** — no Maestro/device harness runs in CI for any slice (programme-level Outstanding), and it bites here: the store↔native 1:1 the sync guard defends can only truly break on a real `TrackPlayer`.

## MUTATION AUDIT

Stryker (`npx stryker run`), scoped to the eleven authored source files, jest runner with `enableFindRelatedTests` + `perTest` coverage, `disableTypeChecks`. Sandbox kept outside `rootDir` (`tempDirName: ../../.stryker-tmp`) per the slice invariant.

| file | score | killed | survived | no-cov |
|---|---|---|---|---|
| `derivePlaybackState.ts` | 100.00 | 21 | 0 | 0 |
| `nativeQueueLock.ts` | 100.00 | 1 | 0 | 0 |
| `nativeSyncGuard.ts` | 100.00 | 18 | 0 | 0 |
| `nativeTrack.ts` | 100.00 | 13 | 0 | 0 |
| `signals.ts` | 100.00 | 34 | 0 | 0 |
| `playbackRateStore.ts` | 100.00 | 6 | 0 | 0 |
| `resumeQueue.ts` | 96.30 | 26 | 1 | 0 |
| `lyrics-sync.ts` | 94.12 | 32 | 2 | 0 |
| `playbackErrorStore.ts` | 94.12 | 16 | 1 | 0 |
| `sleepTimerStore.ts` | 93.33 | 14 | 1 | 0 |
| `audioPrefetch.ts` | 23.08 | 39 | 16 | 114 |
| **logic units (all but audioPrefetch)** | **97.31** | **181** | **5** | **0** |
| **all eleven (covered)** | **91.29** | **220** | **21** | **114** |

### Survivors on the logic units — all equivalent, argued

- **`resumeQueue.ts:13` `if (validTrackIds.length === 0) return 0` → `if (false)`.** With the guard removed, an empty valid set falls through to `Math.max(0, Math.min(savedCurrentIndex, -1))`, which is `0` for every `savedCurrentIndex`. The early return and the fallthrough agree on every input. Equivalent.
- **`sleepTimerStore.ts:18` `if (endsAt === null) return 0` → `if (false)`.** A null `endsAt` coerces to `0` in `Math.ceil((0 - now) / 60_000)`, which is negative for any positive `now`, and `Math.max(0, …)` returns `0`. The guard and the arithmetic agree. Equivalent.
- **`lyrics-sync.ts:5` loop bound `i < lines.length` → `i <= lines.length`, and `:7` `line === undefined || …` → `false || …`.** These are a matched pair: the `line === undefined` guard exists precisely to make the off-by-one bound safe. Under the real `<` bound `lines[i]` is never undefined, so the guard never fires and cannot be observed; under the mutated `<=` bound the guard catches the overshoot and preserves the result. Neither is distinguishable by any lyric input. Equivalent (defensive).
- **`playbackErrorStore.ts:26` `key != null && s.key === key` → `true && s.key === key`.** The `key != null` short-circuit only changes the result when `key` is null while `s.key` is also null and `s.message` is non-null — but `message` is non-null only when `key` was non-null (`report` sets both together, `clear` nulls both). That state is unreachable, so the two agree. Equivalent.

### Survivors on `audioPrefetch.ts` — deferred domain

39 mutants killed on the native-slot swap/repair paths this pass covers. The 16 covered survivors and 114 no-coverage mutants sit in the deferred disk-cache internals (`toStreamingNative`'s presigned-vs-header branch, `upcomingSlotOf`'s `catch`/`i > after` boundary, `refillSlot`'s library-only `swappedToLocal.add`, `findCached`, `extFromUrl`, `evict`'s keep-window, `prefetchNext`'s download round-trip, the `CACHE_SUBDIR` literal). They belong to the deferred on-disk-cache work item above, not to a claim made and unmet here.

## LEFT DARK (deliberate)

The whole of the slice's `.tsx` UI and the disk-prefetch cache are out of this pass's scope — recorded above under Deferred with follow-up tickets, so a later reader can tell a reasoned deferral from a hole.
