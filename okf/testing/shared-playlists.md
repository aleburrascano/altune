---
type: TestSelection
title: Test selection — shared/playlists
description: Which of the twenty taxonomy categories apply to the playlist write module and its picker, which were rejected and why.
resource: apps/mobile/src/shared/playlists/
tags: [testing, mobile, shared, playlists, optimistic-updates]
---

SLICE: `apps/mobile/src/shared/playlists/`
TAXONOMY: [test-taxonomy](../playbooks/test-taxonomy.md)

New slice, 2026-08-01. Most of it is a move — `usePlaylistMutations.ts` and the two components came out of `features/library` unchanged in intent — but the membership hooks were rewritten from single-track to batch, and `AddToPlaylistSheet` gained the `resolveTrackIds` seam. The categories below are derived against the surface as it now stands, not against what moved.

## SELECTED

- **Derivation** — the skipped-count message: `added < requested` produces "N tracks were already in <name>", `added === requested` produces silence. The pluralisation and the playlist-name lookup are both computed, and a wrong verdict here tells the user their add failed when it did not. `__tests__/mutations.test.tsx`.
- **Reducer** — the optimistic cache transitions: `track_count` bumped by the size of the selection on the acting playlist and left alone on every other one; the detail cache filtered to exactly the surviving tracks. Asserted against seeded non-trivial state (two playlists, three tracks), not an empty cache. `__tests__/mutations.test.tsx`.
- **Failure injection** — a rejected add and a rejected remove, each asserting the cache is restored to its pre-mutation value. Rollback is the whole reason `onMutate` returns a context; an optimistic update whose failure path is untested is a data-loss bug waiting for a flaky network.
- **Invalidation** — `onSettled` hitting both `playlistKeys.list` and `playlistKeys.detail(playlistId)` on every membership write, because a membership change alters the grid's count and preview artwork as well as the open detail screen.
- **Functional / acceptance** — the two requirements the slice exists to serve, asserted through the sheet: picking a playlist adds *the resolved ids* (so a track saved on pick reaches the playlist — issue #18), and one gesture over N tracks produces exactly one request (issue #20/#22). The second is asserted with `toHaveBeenCalledTimes(1)`, which is the only assertion that distinguishes a batch client from one that loops.
- **Adversarial** — the seam's three degenerate inputs: a thunk that rejects, a thunk that resolves empty, and a sheet that is not visible. Each asserts the add endpoint is *never* called. A rejected save followed by an add would address an id that does not exist; an empty resolve would send `track_ids: []` and take a 400.
- **Invariant / architecture** — `useCreatePlaylistWithTracks` keeps the created playlist when only the add fails. Rolling the playlist back would discard work the user asked for, so "a failed add is a success with a note" is a rule, not an implementation detail, and it carries a test.
- **Regression** — the batch rewrite's premise: a track already in the playlist is reported as skipped rather than surfacing as an error. The singular endpoint answers 409 on a duplicate, so this is the behaviour change the whole slice turns on.

## REJECTED

- **Table** — no branching pure function large enough to enumerate. `alreadyThereMessage` is two cases and both are covered by Derivation.
- **Property** — no ordering or set-algebra invariant to generalise. The membership algebra lives in the Go aggregate and is tested there; the client sends a list and reads counts.
- **Persistence round-trip** — no I/O of its own. The query cache is in-memory and `@shared/api-client/playlists` is mocked at the module boundary.
- **Security** — no secret handled. Auth is injected by `apiFetch`, tested once in `shared/api-client`.
- **Timing / dwell** — the sheet's 700ms confirmation delay before auto-close is cosmetic; asserting it would pin an animation constant, not a behaviour.
- **Concurrency / ordering** — the row list is inert for the whole resolve-then-add sequence via the shared `busy` flag, so there is no interleaving to construct. React Query serialises the mutation itself.
- **Legacy / compat** — no persisted shape and no wire version to be backwards-compatible with; the slice is new.
- **Device e2e** — deferred to the spine flow.
- **Cross-surface contract** — belongs to `shared/api-client`, where the four new playlist DTOs are field-set-checked against the Go structs. Restating it here would be a second copy of one assertion.
- **Accessibility** — the sheet's rows now carry `accessibilityRole`, a label naming the playlist and its count, and a disabled state, but the assertion belongs with the component-level pass named under DEFERRED rather than as an isolated test here.

## DEFERRED

- **A component-level accessibility pass** over `AddToPlaylistSheet` and `CreatePlaylistModal` — labels asserted, focus order, screen-reader announcement of the "Added ✓" confirmation. The sheet's playlist rows were missing `accessibilityRole`/`accessibilityLabel` entirely when this record was first derived (they moved from `features/library` that way, and the project's own detail-feature rule would have failed them); that was fixed in the same change rather than deferred, but nothing yet asserts it.
- **Component-level tests for `CreatePlaylistModal`.** Covered only indirectly, through the create-and-add path in `mutations.test.tsx`.

## MUTATION AUDIT

Not yet run. The slice is new and has no Stryker entry; the next `npm run mutate` establishes its baseline, and until then no claim in the SELECTED list above has been checked for vacuity.
