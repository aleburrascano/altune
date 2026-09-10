---
type: TestSelection
title: Test selection — features/library
description: Which taxonomy categories apply to the mobile library feature slice, which were rejected or deferred and why, and the scoped mutation result. Logic units 0% → 99.55% mutation score over 221 covered mutants; the Track-not-Song vocabulary invariant is now mechanically enforced; the .tsx UI and the TanStack query hooks are deferred with follow-up reasons.
resource: apps/mobile/src/features/library/
tags: [testing, mobile, feature, library, derivation, invariant, vocabulary, server-grouped-reads]
verified_commit: a0b60ff5471c3cec1411fa82b7ce3a63e3c031a4
---

SLICE: `apps/mobile/src/features/library/`
TAXONOMY: the global workflow taxonomy (`~/.claude/workflow/taxonomy.md`), per issue #173 (epic #113), following the worked example `okf/testing/features-playback.md` from #126.

Authored 2026-09-10 for issue #173. The slice carried a single pre-existing suite (`useSelection`) and its CLAUDE.md named no logic-unit tests. Scope is the slice's testable-in-isolation units — the pure functions and the one debounce hook — plus the Track-not-Song vocabulary rule, which is now a test rather than a comment. The `.tsx` screen/row/grid/modal surface and the TanStack-Query wiring hooks are deferred with reasons and follow-up notes, not silently omitted.

**STATUS: partial-by-design.** Baseline: typecheck green, 1 suite / 11 tests (`useSelection`). After: typecheck green, **9 suites / 81 tests** (8 new files, 70 new tests). Scoped Stryker over the seven mutated logic files: **99.55% of covered mutants killed** (220 killed / 1 survived / 0 no-coverage / 0 errors). The lone survivor is argued equivalent below. `features/library` is **not** added to the CI Stryker gate: the committed `stryker.config.json` mutate glob is `src/shared/**` only, and this slice's residual work is in `.tsx` UI and the query hooks — the unresolved `.tsx`-gate question the programme parks separately (out of scope for #173).

Incidental in-slice fix: `ui/FeaturingScreen.tsx:30` had a pre-existing TS2493 (indexing `useSegments()`'s tuple at `[1]`) that made the whole-project typecheck red on the base commit. A zero-runtime widening cast (`(segments as readonly string[])[1]`) restores a green typecheck without changing behaviour. No proving test is owed — the runtime is unchanged; `.tsx` behaviour testing stays deferred below.

## SELECTED

- **Derivation** *(live domain — the `'ready'` playability literal)* — the acquisition-status `'ready'` gate is the slice's inlined playability literal, and it drives what a user may do to a track. `buildSelectionActions` (`ui/selectionActions.ts`) and `buildTrackMenuItems` (`ui/trackMenu.ts`) are already extracted, testable builders. The truth tables over their outputs are covered: the offline/queue actions enable only when the selection holds a ready track and the download label flips to "Remove download" only when *every* ready track is already pinned; the track menu offers Play Next / Add to Queue / the offline item / Re-acquire only for a ready track, and each withheld arm is proven to add **nothing** (the exact item list is asserted, so a spurious element is caught). `state.ts` `_viewForState` derives the screen view and is covered as a precedence table (below).
- **Table** — every pure function with two or more branches gets its branches and boundaries as rows: `_viewForState` (loading > error > empty > list, each arm asserted to *disagree* with the arm it suppresses, plus the async `'ready'` → `'list'` remap); `coverColumns`/`avatarColumns` (the exact 700 and 1000 breakpoints, inclusive lower bound, and the value just below each); `cellSize` (clean division, the `Math.floor` of a non-integer, and the gap counted `columns − 1` times so a single column subtracts none); the `offlineItem` status ladder (`ready`→Remove, `queued`/`downloading`→Cancel, `failed`→Retry, absent→Download).
- **Contract** — `albumToDiscoveryResult`/`artistToDiscoveryResult` (`ui/library-to-discovery.ts`) consume the server's already-grouped `AlbumGroup`/`ArtistGroup` and map them to a `DiscoveryResult` without re-deriving anything: fixed fields mapped, `year` spread only when `!= null` (0 survives the guard, null is omitted), `track_count` always carried on an album and never borrowed onto an artist. `ui/sort.ts`'s option keys are asserted to be exactly the `LibrarySort` wire values the server applies (`recent`/`az`/`year`), never re-typed literals — labels are display-only.
- **Timing / dwell** *(the search debounce)* — `useLibrarySearch` holds the committed query empty *through* the 300 ms window (asserted at 299 ms) and commits only once it elapses; a keystroke restarts the window so a burst commits once, for the final value; the 2-character minimum is asserted at 1 (never commits) and exactly 2 (commits), and trimming keeps whitespace from counting toward the minimum or reaching the server. `onSubmit` bypasses the debounce and cancels a pending timer; `onClear` resets both fields and cancels.
- **Invariant / architecture** *(live domain — the banned-noun surface)* — the vocabulary rule "the noun is **Track**, never Song" is now mechanically enforced: `noun-invariant.test.ts` scans every shipped `.ts`/`.tsx` file in the slice and fails if `\bsongs?\b` appears, with a guard test asserting the walk actually found files (so a broken walk cannot pass by scanning nothing). Reintroducing "Song" in any chip or list label fails CI.
- **Regression** — the mutation survivors this pass turned up and killed: the unpressed `onPress` handlers on Play Next / Add to Queue / Cancel download (arrow-function mutants), the junk-array (`["Stryker was here"]`) mutants on every withheld menu arm (killed by exact-list assertions), and the unasserted "Add to Playlist" / "Add to Queue" labels.
- **Mutation audit** — see below.

## REJECTED

- **Reducer** — this slice owns no `(state, event) → state` machine. `useSelection` is the closest, and it is a `useState`-backed selection helper already covered by its pre-existing suite (carried, not re-decided); the sort/search state are flat setters covered under Timing/dwell and the `useSelection` carry.
- **Property** — no unit here holds a law over an unbounded generated input space. The collection ordering/grouping law lives on the server, not on the device (see Invalidation/Contract).
- **Legacy / compat** — no unit here loads data written by an older client. The pinned-index narrowing that tolerates historical shapes is `shared/offline/pinnedStore`'s (its own slice), read here only through live `status`.
- **Persistence round-trip** — the slice owns no disk state. Selection is in-memory; the pinned index is `shared/offline`'s.
- **Migration & rollback** — no persisted shape changes in this slice.
- **Error contract** — the tested units surface no typed failure classes; query/mutation error handling is the TanStack hooks' (deferred) and the playlist-write policy is `@shared/playlists`'.
- **Invalidation / Liveness** — the library reads subscribe to TanStack Query keys, but the tested logic units touch no cache key by identity; invalidation is exercised where the query hooks live (deferred), and the CLAUDE liveness rule bites on the `.tsx` components (deferred).
- **Idempotence / replay · Resource lifecycle** — no unit here replays an input or acquires a releasable resource; the pinned-download lifecycle is `shared/offline`'s.
- **Adversarial · Failure injection · Concurrency / ordering · Load & degradation · Performance budget · Configuration** — no unit here crosses a trust boundary, performs failable I/O, interleaves, faces a capacity limit, holds a latency bound, or reads environment config. The one debounce timer is deterministic under fake timers (Timing/dwell).
- **Security** — no secret is handled by the tested units. The bearer token is attached far downstream in `shared/api-client`; the builders here pass domain objects only.
- **Observability** — the tested units emit no diagnostic channel; user-facing failure surfaces belong to the deferred `.tsx` states and the query hooks.
- **Functional / acceptance · Accessibility · End-to-end** — user-visible through the `.tsx` screen and its selection bar / context menu / sheets, all deferred below. No device/Maestro harness runs in CI for any slice (programme-level Outstanding).

## DEFERRED (with follow-up)

- **The library `.tsx` surface** — `LibraryScreen` (including its local `sortPlaylistsByKey` pure helper, which is *not* a library-list re-derivation but the owned-playlists collection's sort), `LibraryChips`, `SortControl`, `LibraryNoResults`, `PlaylistsGrid`/`TracksList`/`AlbumsGrid`/`ArtistsGrid`/`LibraryRow`, `PlaylistDetailScreen`/`PlaylistHero`, `AddTracksToPlaylistModal`, `SelectionBar`, `FeaturingScreen`, `LibraryHeader`, `PlaylistCover`. Derivation-in-a-component, Liveness (the CLAUDE "mutate the store, assert the render changed" rule), Accessibility, and Functional/acceptance for the `.tsx` surface. This is the `.tsx` mutation-gate question the programme parks; not resolved here. `sortPlaylistsByKey` and the "never render server-mutable state from a snapshot" liveness checks are the highest-value items when this is picked up.
- **The TanStack-Query wiring hooks** — `useLibraryHome` (`useLibraryTracks`/`useLibraryAlbums`/`useLibraryArtists`/`useLibraryIsEmpty`/`loadAll`), `useTracksFeaturing`, `usePlaylistActions`, `useDeleteTrack`/`useDeleteTracks`, `useReacquireTrack`, `useRetryAcquisition`, `useLibraryNavigation`. Integration-level: they own query keys, `placeholderData: keepPreviousData`, the pending-poll `refetchInterval`, `loadAll`'s whole-collection queue seam, and the optimistic-patch handoff to `@shared/playlists`. These need a `QueryClient` harness and the `__http` double driven with the library endpoints — Invalidation, Failure injection, and the "queue the whole collection, never the loaded pages" invariant. Deferred to a follow-up; the logic those hooks *delegate* to (the builders, the view derivation, the debounce) is covered here.
- **Device e2e** — no Maestro/device harness runs in CI for any slice (programme-level Outstanding).

## MUTATION AUDIT

Stryker (`npx stryker run --mutate "<the seven library logic files>"`), jest runner with `enableFindRelatedTests` + `perTest` coverage, `disableTypeChecks`. Sandbox kept outside `rootDir` (`tempDirName: ../../.stryker-tmp`) per the slice invariant. The committed config's mutate glob is unchanged (`src/shared/**`); this run overrides it on the CLI only, so no slice is added to the gate.

| file | score | killed | survived | no-cov |
|---|---|---|---|---|
| `state.ts` | 100.00 | 10 | 0 | 0 |
| `ui/gridColumns.ts` | 100.00 | 25 | 0 | 0 |
| `ui/library-to-discovery.ts` | 100.00 | 15 | 0 | 0 |
| `ui/selectionActions.ts` | 100.00 | 49 | 0 | 0 |
| `ui/sort.ts` | 100.00 | 34 | 0 | 0 |
| `ui/trackMenu.ts` | 100.00 | 61 | 0 | 0 |
| `hooks/useLibrarySearch.ts` | 96.30 | 26 | 1 | 0 |
| **all seven (covered)** | **99.55** | **220** | **1** | **0** |

An earlier pass measured 95.48% (10 survivors); nine were genuine test weaknesses — unpressed `onPress` handlers (Play Next / Add to Queue / Cancel download), junk-array (`["Stryker was here"]`) mutants on the withheld menu arms, and two unasserted action labels — all killed by pressing the handlers, asserting the exact menu composition, and asserting the labels.

### Survivor — equivalent, argued

- **`useLibrarySearch.ts:21` `if (debounceRef.current)` → `if (true)`.** The guard only avoids calling `clearTimeout` when the ref is already null. `clearTimeout(null)` is a defined no-op and the following `debounceRef.current = null` is idempotent, so on the null path the guarded and unguarded code produce identical observable behaviour, and on the non-null path they are identical by construction. No input distinguishes them. Equivalent (defensive).

## LEFT DARK (deliberate)

The whole `.tsx` UI surface and the TanStack-Query wiring hooks are out of this pass's scope — recorded above under Deferred with reasons, so a later reader can tell a reasoned deferral from a hole. `sortPlaylistsByKey` inside `LibraryScreen.tsx` is a pure function that would be a clean Table target once the `.tsx` surface is picked up; it is left with the rest of that file rather than extracted mid-ticket.
