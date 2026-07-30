---
type: TestSelection
title: Test selection — shared/acquisition
description: Pre-filled Regression candidates for the acquisition slice, harvested from the 2026-07-29 mutation audit before its suite was deleted. Category selection not yet run.
resource: apps/mobile/src/shared/acquisition/
tags: [testing, mobile, shared, acquisition, download]
verified_commit: 98dcd6a8b6c41a7ceeab71d24771c1752819a8eb
---

SLICE: `apps/mobile/src/shared/acquisition/`
TAXONOMY: [test-taxonomy](../playbooks/test-taxonomy.md)

**STATUS: not started.** This record exists ahead of the work because the findings below were produced by an agent-driven mutation audit on 2026-07-29, against a suite that was deleted on 2026-07-30. They live nowhere else. Everything in `## REGRESSION CANDIDATES` is a defect that survived a mutation — a bug that has not shipped yet — and each one is owed a test asserting the intended behaviour.

Run `/qa-slice apps/mobile/src/shared/acquisition/` to start. Step 2 fills in SELECTED / REJECTED / DEFERRED; the blind authors must not read this file (they may read only `okf/playbooks/test-taxonomy.md` under `okf/`).

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

## KNOWN CONTEXT

- `trackStatusStore.ts` had **no dedicated test file** before the reset, despite being the overlay behind two written rules in `apps/mobile/CLAUDE.md`: ownership is read from the server stamp overlaid with this store for liveness, and a component showing server-mutable state gets a liveness test.
- `fallow health` ranks `trackStatusStore.ts` and `downloadStore.ts` as high-impact (5 dependents each) — a change here amplifies.
- The re-acquisition flow is the app's most recently changed area and the subject of four consecutive fixes in the log, so **Legacy/compat** and **Idempotence/replay** deserve hard looks.
- `applyServerEvent.ts` drives this store from SSE and is already at 93.68% mutation score, so the event-side contract is constrained; what is missing is this store's own behaviour.
