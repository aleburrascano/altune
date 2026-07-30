---
name: qa-slice
description: Drive one Altune slice to real coverage — map dark code, author production-grounded tests in parallel, then break the source to kill vacuous tests.
disable-model-invocation: true
---

# QA a slice

One **slice** per run: a mobile feature (`apps/mobile/src/features/<name>/`), a mobile shared subsystem (`apps/mobile/src/shared/<name>/`), or a Go module (`services/go-api/internal/<name>/`). Repo-wide coverage is many runs of this skill, never one.

Three agents do the work. You orchestrate, you do not write tests yourself.

| agent | job |
|---|---|
| `test-cartographer` | finds **dark** lines, classifies each as live / dead / impossible |
| `test-author` | writes tests for one work unit — run these in parallel |
| `test-assassin` | breaks source under passing tests, reports **survivors** |

## The bar

Coverage is a pointer, not a goal. 100% of statements is trivially reachable by tests that assert nothing, and those tests are worse than no tests — they make the number lie. The real bar is three conditions, all required:

1. Every category the taxonomy selects for this slice is satisfied at its **done-condition**, or recorded as deferred with what blocks it.
2. Every **live** dark region is covered, or explicitly recorded as not covered with a reason.
3. Zero **survivors** — no mutation of a covered line leaves the suite green.

Dark code that is dead or impossible does not get a test. It gets a finding.

## Steps

### 1. Fix the target

If the user named a slice, use it. Otherwise run coverage across both surfaces, rank slices by uncovered statements, and ask which one. Do not pick silently.

Record the baseline number before anything changes. You will compare against it in step 7.

### 2. Select categories

Read `okf/playbooks/test-taxonomy.md` and walk all twenty categories against this slice. For each, decide **selected**, **rejected**, or **deferred** — applicability comes from the *Applies when* column, a property of the code, not from what feels worth testing.

Write the selection record to `okf/testing/<slice>.md` in the format the taxonomy specifies, including every rejection and its reason.

Done when: all twenty categories carry a verdict. A category silently omitted is indistinguishable from a hole — that is the failure mode this step exists to prevent.

### 3. Cartograph

Spawn one `test-cartographer` on the slice, passing the coverage output. Wait for it.

Done when: every uncovered line in the slice carries a verdict, and the live ones are split into work units. If the map has gaps, send it back — do not start authoring from a partial map.

### 4. Author, in parallel — blind

Spawn one `test-author` per work unit, **all in a single message** so they run concurrently. Give each: its unit's files, its live items with the production events the cartographer named, the slice path, and **the categories step 2 selected for those files, with their done-conditions quoted**.

Authors work **blind**, and the prohibition has exactly one exception. Copy this into every brief:

> Do NOT read anything under `okf/` — with one exception: `okf/playbooks/test-taxonomy.md` IS required reading, because it is the instruction set rather than knowledge about this code. Do NOT recover deleted tests via `git show` / `git log` / `git diff`. Do NOT read a nested `CLAUDE.md` for its test-file list (its architectural rules are fine). Derive everything else from the source under test and the types it imports.

Blindness is not ceremony. A prose-guided author reproduces the blind spots of whoever wrote the prose — which is how a slice ends up with a green suite that never exercised two of its fourteen event types. Tell each author its own harness facts explicitly (runner command, doubles and their control surface, which libraries are installed) so it does not have to rediscover infrastructure it is not allowed to read about.

Also require of each author: **report anything that looks wrong, fragile, or contradictory in the source, and do not fix it.** This is not a courtesy — on `shared/events` it was the highest-value output of the whole run, and it finds a class of defect mutation testing provably cannot (see the taxonomy's note on equivalent mutations).

Done when: every work unit reports back, every live item is either mapped to a named test or listed under `NOT COVERED` with a reason, and every selected category for that unit has its done-condition met or is escalated as deferred. A dropped live item is a failed run, not a rounding error.

### 4b. Resolve every finding, and verify it yourself

Do not relay an author's finding as fact. Read the source and confirm it before acting — and where a fix depends on how a value is consumed, read the consumer too.

For each confirmed defect, either fix the source or mark the test `it.failing()`. Never accept a passing test that asserts the broken behavior; see the taxonomy's rule on this.

### 5. Assassinate

Spawn one `test-assassin` per authored test file, again all in one message. Include the cartographer's **vacuous suspects** as extra targets — pre-existing tests get audited in the same pass.

Done when: every assassin reports `TREE CLEAN: yes`. Verify yourself with `git diff --stat` — no source file outside `__tests__/` or `*_test.go` may show changes. If one does, revert it before continuing.

### 6. Repair

For each survivor, spawn the authoring agent again to strengthen the test that let it through, then re-run the assassin on that file only.

Loop until zero survivors — or until a survivor is genuinely a source defect rather than a weak test, in which case leave it, and carry it into the report as a bug.

Do not accept a survivor because "the test is close enough." That is exactly the failure this skill exists to catch.

### 7. Report

Run the full suite for the slice, plus `npx tsc --noEmit` (mobile) or `go vet ./...` (Go). Update `okf/testing/<slice>.md` with the final mutation numbers. Then report:

```
SLICE: <path>
COVERAGE: <before>% → <after>%
TESTS ADDED: <n> across <n> files
KILL RATE: <killed>/<applied> mutations
TAXONOMY: <n> selected — <n> satisfied, <n> deferred; <n> rejected with reason
SURVIVORS: <n> found, <n> killed, <n> left as source bugs

BUGS FOUND
- <source defects the tests or mutations exposed>

DEAD CODE (proposed for deletion — not deleted)
- <file>:<lines>

LEFT DARK (deliberate)
- <file>:<lines> — <the invariant that makes it unreachable>
```

Never delete dead code as part of this run. Surfacing it is the deliverable; removing it is the user's call.

## Constraints

- **Never run `go test -race` locally** — Windows, no C toolchain. CI runs it on Linux.
- `AIDEV-NOTE` / `DECISION` / `WARNING` anchors are durable; nothing here strips them.
- `okf/` concepts have a pre-commit staleness hook. If this run changes behavior a concept describes, update the concept in the same commit.
- Commits are Conventional Commits, scope from `commitlint.config.js`, and carry no Claude co-author trailer.
