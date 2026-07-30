---
type: TestSelection
title: Test selection — shared/events
description: Which of the twenty taxonomy categories apply to the mobile SSE event bus, which were rejected and why, and the mutation result.
resource: apps/mobile/src/shared/events/
tags: [testing, mobile, shared, sse, events]
verified_commit: 98dcd6a8b6c41a7ceeab71d24771c1752819a8eb
---

SLICE: `apps/mobile/src/shared/events/`
TAXONOMY: [test-taxonomy](../playbooks/test-taxonomy.md)

Rebuilt blind on 2026-07-30: the authors were given the source and the taxonomy only, and were prohibited from reading `okf/`, the deleted tests in git history, or any nested `CLAUDE.md` test list. The point was to see which cases a fresh derivation would find that the previous suite had missed. It found nine defects; see [shared-events](../mobile/shared-events.md).

## SELECTED

- **Table** — `progressPhase`'s six named stages plus unknown and missing; `parseAddedTrack`'s required-field guard; the wire-field parser. `__tests__/applyServerEvent.test.ts`, `__tests__/sse-client.test.ts`.
- **Reducer** — all 14 `ServerEventType`s against seeded non-trivial cache state, enumerated from `SERVER_EVENT_TYPES` rather than from the handler's branches. `__tests__/applyServerEvent.test.ts`.
- **Property** — `reorderPlaylistCache` preserves membership and count over any shuffled subsequence; `patchTrackInCaches` replay-idempotence over generated patches (`fast-check`). `__tests__/playlistCachePatch.test.ts`, `__tests__/trackCachePatch.test.ts`.
- **Cross-surface contract** — `__tests__/eventContract.test.ts` derives the published event set from the Go source at test time (`git ls-files *.go`, every `.Publish(…, "name")` literal plus the handler's `event: resync`) and asserts equality with `SERVER_EVENT_TYPES` in both directions. Derived, never restated.
- **Invalidation** — exact query keys asserted by identity for `resync`, `track_deleted`, the thin-payload fallback, and every `INVALIDATION_MAP` entry.
- **Idempotence / replay** — `track_added_to_library`, `track_deleted`, `track_removed_from_playlist`, and both cache patchers applied twice, asserted equal to once. This is the category that caught the `track_count` divergence.
- **Adversarial** — one missing required field at a time, wrong JSON types, non-array `track_ids`, duplicate ids, malformed frames, bad JSON beside a good block, comment and `retry:` lines, blocks split byte-by-byte.
- **Failure injection** — transport error, `onloadend`, `MAX_RESPONSE_BYTES` recycle, a rejected `getToken()`, a null token.
- **Concurrency / ordering** — two overlapping `connect()`s, the error+loadend double-fire reentrancy guard, disconnect racing reconnect, dispose racing an in-flight token.
- **Timing / dwell** — watchdog asserted at the exact boundary and mid-window; backoff delays asserted exactly, including the jitter range `[base, 2*base)`.
- **Legacy / compat** — `Last-Event-ID` sent, omitted, and preserved across a recycle; thin `track_id`-only payloads.
- **Regression** — the nine defects this rebuild found each carry a test asserting the intended behavior.
- **Mutation audit** — see below.

## REJECTED

- **Derivation** — no display or gating state is computed here; this slice has no components.
- **Persistence round-trip** — the reducer performs no I/O of its own. `repinIfPinned` and `downloadStore` own their persistence and are tested in their own slices.
- **Security** — no secret is handled here. The bearer token is read from the Supabase session and set as a header; `shared/auth` owns it.
- **Accessibility** — no rendered output.
- **Functional / acceptance** — no user-visible requirement lives at this layer; the requirements it serves ("a save on another device appears here") belong to the feature slices that render the caches.
- **Device e2e** — deferred to the spine flow, not this slice.
- **Invariant / architecture** — the one rule that applies (every published event is handled) is already enforced by Cross-surface contract plus the `Exclude<ServerEventType, …>` compile-time exhaustiveness in `applyServerEvent.ts`.

## DEFERRED

- **`repinIfPinned`'s async fan-out** inside `track_acquisition_completed`. Driving it past its synchronous no-op would pin `pinnedStore`'s microtask scheduling rather than this reducer's contract. Unblocks when `shared/offline` is rebuilt and exposes a controllable queue.

## MUTATION AUDIT

Pre-rebuild baseline (2026-07-29, against the deleted suite): 9 mutations applied, **0 killed**, 9 survived.

Post-rebuild (2026-07-30, Stryker over 563 mutants in the slice's 6 source files, 3m44s):

| file | score | killed | survived |
|---|---|---|---|
| `eventTypes.ts` | 100.00 | 5 | 0 |
| `playlistCachePatch.ts` | 95.77 | 68 | 3 |
| `applyServerEvent.ts` | 93.68 | 178 | 11 |
| `trackCachePatch.ts` | 90.98 | 121 | 11 |
| `sse-client.ts` | 80.99 | 115 | 26 |
| `useServerEvents.ts` | 76.19 | 16 | 5 |
| **total** | **89.50** | **503** | **56** |

**0% → 89.50%.** `thresholds.break` in `stryker.config.json` is set to 89 and is raise-only from here, on the same terms as the coverage floors.

The remaining work is concentrated: `sse-client.ts` and `useServerEvents.ts` hold 31 of the 56 survivors. One of them is worth calling out because it is a genuine gap rather than an equivalent mutation — changing `useEffect`'s dependency array from `[queryClient]` to `[]` survives, so nothing constrains the effect's re-subscription behavior when the query client changes. Coverage for both files is high; that is exactly the divergence the kill rate exists to expose.

`test-assassin` is retained alongside it, not replaced by it: Stryker gives the reproducible number a ratchet needs, the agent gives the semantic mutations no generator produces — swapping two adjacent same-typed fields, reversing a spread so the stale copy wins, committing before the send is confirmed. Those three found the worst defects in the 2026-07-29 audit and none of them are in a standard mutator's repertoire.

Note on method: the blind rebuild and the mutation audit find **disjoint** defect classes. Mutation asks whether the suite notices a change and so finds weak tests over correct code; blind derivation asks whether the code is right and so finds wrong code. The `failure_message` defect proves the gap — the field is never sent by the server, so replacing its read with `null` is an equivalent mutation and no mutation runner can surface it.
