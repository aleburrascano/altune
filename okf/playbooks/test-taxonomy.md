---
type: Playbook
title: Test taxonomy
description: The twenty categories of test a slice can deserve, each with the code property that triggers it and the condition that closes it — the input to qa-slice's category selection.
resource: .claude/skills/qa-slice/SKILL.md, .github/workflows/test-backend.yml
tags: [testing, mutation-testing, coverage, quality-gates, methodology]
verified_commit: 98dcd6a8b6c41a7ceeab71d24771c1752819a8eb
---

This list exists because of one measurement. On 2026-07-29 a mutation pass ran 72 mutations against six `apps/mobile/src/shared/` subsystems under a suite of 552 passing tests. 26 mutations were killed. 43 survived — each one a change to production code that would ship a user-visible defect and that the suite did not notice. Kill rate: 38%.

The suite was not sloppy. It had zero snapshot tests, dense assertions, and one genuinely excellent cross-surface contract test. What it lacked was any statement of *which constraints a piece of code deserves*. Coverage was the target, so coverage is what it achieved: every survivor sat on a line a coverage report counted as covered.

A coverage percentage answers "did this line run." This taxonomy answers "which constraints does this code deserve, and do they exist." That is the question the 43 survivors were hiding behind.

## How to use it

Per slice — a mobile feature (`apps/mobile/src/features/<name>/`), a mobile shared subsystem (`apps/mobile/src/shared/<name>/`), or a Go module (`services/go-api/internal/<name>/`):

1. Walk all twenty categories. For each, decide **selected**, **rejected**, or **deferred**.
2. Applicability is derived from the *Applies when* column — a property of the code — not from judgment about what feels worth testing.
3. Write the decision down, including every rejection and its reason, in the slice's selection record (format at the bottom). Commit it.
4. Author against the *Done when* column. A category is not satisfied because tests exist in its shape; it is satisfied when its done-condition holds.
5. Run **Mutation audit** last. Until it passes, no other category's claim to be satisfied is verified.

A rejection with a reason is a finding. A category silently omitted is a hole, and it is indistinguishable from the 43.

## When authoring finds a bug

A test asserts **intended** behavior. Never a defect.

Writing tests to a done-condition surfaces bugs — that is a feature, not an interruption. When it happens there are exactly two legal moves:

1. **Fix the source**, and let the test assert what it should always have asserted.
2. **Mark the test `it.failing()`** and record the defect. Jest inverts it: the test passes while the bug exists and fails the moment someone fixes it, which forces the assertion to be promoted rather than silently kept.

The illegal move is a green test that encodes the broken behavior. On the 2026-07-30 rebuild of `shared/events` an author produced a passing test named *"never flushes a block terminated by CRLF CRLF"* — a real wire-format bug, recorded as an expectation. That test does not protect the code, it protects the defect: fix the parser and it goes red, and the cheapest response is to "fix" the test. A defect written as an expectation is worse than no test, for the same reason a vacuous test is worse than none — it makes the suite assert something false.

If a finding turns out not to be a defect, say so and move on. If a fix depends on how a value is consumed, read the consumer before choosing it: the same rebuild nearly shipped a `failure_message` fallback to the raw `reason` code, which `LibraryRow` would have rendered to users as the literal string `no_candidates` instead of its own "Acquisition failed" copy.

## What this is not

**Not a checklist to maximize.** Some categories are actively wrong for some code. Chasing all twenty over `apps/mobile/src/shared/ui/primitives/` rebuilds vacuous coverage with more ceremony.

**Not a line-count target.** Lines of test code is the wrong meter and optimizing it selects against the best techniques in this list: one property is a handful of lines and constrains an unbounded input space, where the equivalent hand-written table is ten times longer and constrains only the rows someone thought of. Prefer the shorter, stronger form every time. The meters that matter are mutation kill rate and the count of categories satisfied versus rejected-with-reason.

**Not a replacement for reading the code.** It is what makes not reading the code defensible — and only once its done-conditions hold and the mutation audit is clean.

## Logic

Every slice with behavior needs this family. If nothing here is selected, the selection is wrong.

| Category | Applies when | Proves | Done when | Why it's here |
|---|---|---|---|---|
| **Table** | a pure function with two or more branches | each branch computes the right value, at its boundaries | every branch and every boundary is a row, including the exact value a comparison turns on | `progressPhase` lost its `finishing` arm undetected — the suite only ever sent `download` and one bogus stage |
| **Derivation** | display or gating state is computed inside a component | the rules deciding what shows and what unlocks | the truth table over the derived outputs is covered — extract the derivation to a pure function first | `AuthForm.tsx:60-69` computes four display/gating booleans reachable only by rendering and driving text inputs |
| **Reducer** | a store, or a `(state, event) → state` function | every legal transition, not a diagonal through them | every state × event pair the domain permits has a case; illegal pairs are asserted as rejected | `applyServerEvent` handles 14 event types and killed 0 of 9 mutations; `playlist_created` and `playlist_deleted` are sent by no test in the repo |
| **Property** | an invariant holds across an input space too large to enumerate | the invariant itself, over generated input | the property is stated as a law and generated against; shrunk counterexamples become Table rows | `queueStore.playOrder` must always be a permutation of `tracks` indices — one property kills several index-arithmetic survivors at once. No property-based library is installed on either surface |

## Data and contracts

| Category | Applies when | Proves | Done when | Why it's here |
|---|---|---|---|---|
| **Cross-surface contract** | the mobile client consumes a shape or name the Go API produces | client and server agree, and keep agreeing | the contract is **derived** from the source of truth at test time, never restated as a literal | `shared/events/__tests__/eventContract.test.ts` scans the Go source for published event names — the exemplar. Counter-example: `TrackResponse`'s 19 fields are hand-rolled across 17 test files with no shared factory |
| **Legacy / compat** | data written by an older version can still arrive | old shapes still load, and don't corrupt new ones | every historical shape still in the wild has a fixture, including absent and null fields | commit `59d6da5` "make the first replace work on pre-012 tracks". `restoreQueue` clamps and `loadPersistedOutbox` validates precisely because old data is real; neither is tested for it |
| **Persistence round-trip** | state must survive process death | the write actually happened and reads back | a real double round-trips: write → read returns what was written; deleting the write breaks a test | the `expo-file-system` double has `write() {}`, unwired from `textSync()`. Gutting `pinnedStore.saveIndex` and inverting `outboxStore.persistOutbox` both left the suite green — two durability guarantees, one broken seam |

## Wiring and state

| Category | Applies when | Proves | Done when | Why it's here |
|---|---|---|---|---|
| **Invalidation** | a mutation must refresh cached server state | the right cache entries are refreshed, and no others | the exact query keys are asserted, by identity, not by "something was invalidated" | dropping `playlistKeys.details` from `INVALIDATION_MAP.playlist_deleted` survived; `shared/lib/query-keys.ts` has zero references in any test file |
| **Liveness** | a component renders server-mutable state | the screen updates in place rather than from a snapshot | mutate the owning store or cache; assert the rendered output changed | already a mobile CLAUDE.md rule. `LibraryRow.liveness.test.tsx` is the exemplar; the rule exists because stale screens have shipped twice |
| **Idempotence / replay** | an input can be delivered more than once | applying twice equals applying once | `apply(apply(e))` is asserted equal to `apply(e)` for every replayable input | removing the `Math.max(0, …)` clamp in `removeTrackFromPlaylistCache` survived — a replayed removal drives the displayed track count negative |

## Environment

Hostile conditions, as distinct from hostile inputs.

| Category | Applies when | Proves | Done when | Why it's here |
|---|---|---|---|---|
| **Adversarial** | a trust boundary — anything crossing from server, disk, deep link, or provider | malformed input degrades safely instead of being trusted | expired, replayed, tampered, malformed, partial, and thin payloads each have a case | `applyServerEvent`'s required-field guard lost its `!artist` clause undetected — a thin payload upserts a blank-artist track into the library instead of falling back to refetch |
| **Failure injection** | the code performs I/O that can fail | the failure path, not just the success path | every I/O call site has a test where it fails: disk full, network drop mid-transfer, stream disconnect, provider 429, keychain unavailable, killed mid-write | absent for a structural reason — the native doubles always succeed. The entire acquisition and offline domain is flaky I/O over external providers |
| **Concurrency / ordering** | two operations can interleave, or an order matters | the guard, and the ordering | both interleavings are driven and asserted; the reentrancy guard has a test that fails without it | `flushOutbox`'s `_flushing` guard is removable undetected, and swapping commit-before-send is undetected — an event is dropped from disk before delivery is confirmed |
| **Timing / dwell** | a duration is user-visible | the duration, not just the end state | asserted mid-window — the state is still present partway through the intended hold | shortening the "Done" dwell by 500ms survived, because the test only advanced all timers and asserted the final state |
| **Security** | secrets, tokens, or credentials are handled | they stay where they belong | asserted absent from logs, query keys, plaintext storage, and error payloads | "Never store secrets in `AsyncStorage`" and "never decode or manipulate the access token here" are written rules with no test |

## Product

Requirement-facing, and the only family the code cannot generate for you.

| Category | Applies when | Proves | Done when | Why it's here |
|---|---|---|---|---|
| **Functional / acceptance** | a user-visible requirement exists | the product promise, not the implementation | asserted through the public interface in `docs/ubiquitous-language.md` terms, with no reference to internals | `docs/specs/auth-integration/spec.md` claims 13 acceptance criteria; none of them execute. Every one of the 72 mutations asked whether the code does what the code says — none asked whether that is what was promised |
| **Accessibility** | a component is interactive or announces change | it is usable without sight and with assistive tech | labels, roles, and announcements asserted; hit targets checked | 2 of roughly 40 component test files assert anything accessibility-related, despite `shared/ui/announce.ts` and `useAnnounceChange.ts` existing for the purpose |
| **Device e2e** | a spine flow crosses the whole stack | the app boots, navigates, and completes the flow on real hardware | runs on a device in CI, or the deferral is recorded with a reason | three Maestro flows exist; none runs in any workflow. Mocks structurally cannot catch a misconfigured native module or an unresolvable route |

## Meta

| Category | Applies when | Proves | Done when | Why it's here |
|---|---|---|---|---|
| **Invariant / architecture** | a CLAUDE.md rule is mechanically checkable | the rule holds, permanently, without a reviewer | violating the rule fails a test or a linter, in CI | import direction is enforced (eslint zones, depguard); most other written rules are not. Under agent authorship these are the cheapest durable constraints there are |
| **Regression** | a bug reached a user, **or a mutation survived** | that this specific defect cannot return | a test fails on the pre-fix code and passes after | `weak-signal`, `nativeSyncGuard`, `queue-generation`, `service.remotePrevious` encode real incidents and are the suite's irreplaceable memory. A survivor is a bug that has not shipped yet |
| **Mutation audit** | any category above is claimed satisfied | that the tests constrain rather than merely execute | zero survivors, or each survivor recorded with a reason — a source defect, or genuinely equivalent | this is the row that makes the other nineteen real. Without it the taxonomy is a coverage target wearing a longer coat |

## Rejected categories

**Visual regression / screenshot diffing — out.** Its failure mode is the same as snapshot testing: when the baseline breaks, the cheap fix is to regenerate it, and under agent authorship that is what happens. It converts a real regression into a diff that gets waved through. Functional and Device e2e catch the same defects while asserting intent. Do not add it back without an ADR.

**Observability — folded, not rejected.** "Does this failure actually emit telemetry" is asserted inside Functional and Regression rather than as its own category.

**Null-shape coverage — folded into Cross-surface contract.** Its done-condition is the fixture factory carrying a named variant per nullable and optional field, which is where contract drift actually enters.

## The selection record

One per slice, committed at `okf/testing/<slice>.md`, rewritten when the slice's categories change.

```
SLICE: <path>
TAXONOMY: okf/playbooks/test-taxonomy.md @ <verified_commit>

SELECTED
- <category> — <the code property that triggered it> — <where the tests live>

REJECTED
- <category> — <why this code cannot exhibit that failure>

DEFERRED
- <category> — <what is blocking it, and what unblocks it>

MUTATION AUDIT
- <n> applied, <n> killed, <n> survivors carried as source defects
```

The record is the reviewable artifact. Selected without a done-condition met is a lie; rejected without a reason is a hole.
