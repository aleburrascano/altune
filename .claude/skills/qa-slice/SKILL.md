---
name: qa-slice
description: Drive one Altune slice to real coverage in an isolated worktree — map dark code, author production-grounded tests in parallel, break the source to kill vacuous tests, and leave a branch for /qa-integrate.
disable-model-invocation: true
---

# QA a slice

One **slice** per run: a mobile feature (`apps/mobile/src/features/<name>/`), a mobile shared subsystem (`apps/mobile/src/shared/<name>/`), or a Go module (`services/go-api/internal/<name>/`). Repo-wide coverage is many runs of this skill, never one.

This run is **isolated**: it works in its own git worktree, on its own branch, and touches no shared gate. Up to two of these run concurrently. `/qa-integrate` merges the branches and moves the gates afterwards.

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

## Settled — do not re-propose

Each of these was decided with evidence and must not be reopened mid-run. The reasoning lives in [okf/testing/programme.md](../../../okf/testing/programme.md); do not read it to argue with the verdict.

- **Never lower a floor to make a run land.** Raise-only, everywhere, without exception. A floor that yields on demand is not a gate. One lowering exists on record and is documented as a mistake.
- **Never refactor for testability.** Every survivor in the founding audit sat in a function already small and pure enough to test exhaustively. Extraction is justified only where it changes *reachability* — a derivation trapped inside a component — never to move a number.
- **Never chase 100% mutation score.** Guard-clause conditionals and error-message strings are equivalent by this repo's own convention, which forbids asserting error copy. Triage and record; an unexplained number is the coverage trap in a new costume.
- **Never add a snapshot test or visual-regression diff.** When the baseline breaks the cheap fix is to regenerate it, which under agent authorship is what happens.
- **Never fix the static-analysis backlog here.** It is already ratcheted so it can only fall.
- **A test asserts intended behaviour, never a defect.** On finding a bug: fix the source, or mark the test `it.failing()`. A passing test that encodes broken behaviour protects the defect.

## Traps

Every one of these cost a run.

- **Never run two assassins against one working tree.** They share the suite, so each runs it while the other has a mutation applied. Survivors stay trustworthy — a concurrent mutation can only add failures — but every *kill* becomes unattributable, and a kill rate is the number this programme reports. Group by source file and run them one at a time.
- **Prove each new gate fails when it should.** Apply the mutation by hand, watch the intended test go **red**, revert. Passing proves nothing.
- **Take the verdict from the exit code, never the output text.** And when you anchor a mutation to a line, anchor on something unique and assert the anchor matched — a 4-space anchor once matched inside a 6-space line elsewhere in the file, and two results were silently attributed to the wrong call site.
- **Harvest before deleting anything.** A finding that lives only in this transcript is lost when the session ends. Write it to the slice record as you go.
- **Blind means blind.** The one permitted `okf/` file for an author is the taxonomy. A pre-filled selection record is for you, never for them.
- **Never leave an unhandled rejection in a test of a fire-and-forget API.** Mutation empties function bodies, so a `void`-ed call rejects; under Node 25 that kills the *worker* rather than failing the test, and Stryker abandons the run after a handful of crashes. Attach a `.catch()`.
- **A source-scanning test must resolve the repo root, never `__dirname`.** Stryker runs from a sandbox copy with mutant switches inlined, so a `__dirname`-relative read passes under jest and fails the *dry run* — aborting before a single mutant is applied and reporting it as a config error.

## Steps

### 0. Isolate

Do this first, before reading any source. Do not skip it even for a single-slice run — the branch is what `/qa-integrate` consumes.

Call `EnterWorktree` with a name derived from the slice (`features/detail` → `qa-detail`). It branches from `origin/main` and switches you into `.claude/worktrees/<name>/`.

A fresh worktree has no `node_modules` and no generated router types. Both are needed, and neither can be a plain junction — Stryker's sandbox discovery uses `readdir` Dirents, where a junction reports as a link rather than a directory, so it silently fails to symlink `node_modules` and every mutant dies with `Cannot find module 'jest'`. Run this from the worktree root:

```powershell
$main = (Resolve-Path "../../..").Path
$mobile = "apps/mobile"
New-Item -ItemType Directory -Force -Path "$mobile/node_modules" | Out-Null
Get-ChildItem "$main/$mobile/node_modules" -Force | ForEach-Object {
  $t = Join-Path (Resolve-Path "$mobile/node_modules").Path $_.Name
  if ($_.PSIsContainer) { New-Item -ItemType Junction -Path $t -Target $_.FullName -EA SilentlyContinue | Out-Null }
  else { Copy-Item $_.FullName $t -Force }
}
New-Item -ItemType Directory -Force -Path "$mobile/.expo/types" | Out-Null
Copy-Item "$main/$mobile/.expo/types/*" "$mobile/.expo/types/" -Force
```

Done when: `cd apps/mobile && npx tsc --noEmit` exits 0 and `npx jest src/shared/lib` passes. If `tsc` reports `TS2493` in `FeaturingScreen.tsx`, the router types did not copy — fix that before continuing, or you will chase a phantom type error all run.

**Never `npm install` anything.** The packages are shared with the main tree and a concurrent run.

### 1. Fix the target

If the user named a slice, use it. Otherwise run coverage across both surfaces, rank slices by uncovered statements, and ask which one. Do not pick silently.

Read [okf/testing/programme.md](../../../okf/testing/programme.md)'s **Outstanding** section once, so you recognise a known cross-slice defect instead of re-reporting it as new. That is the only file you read for programme state.

Record the baseline coverage number before anything changes.

### 2. Select categories

Read `okf/playbooks/test-taxonomy.md` and walk all twenty categories against this slice. For each, decide **selected**, **rejected**, or **deferred** — applicability comes from the *Applies when* column, a property of the code, not from what feels worth testing.

Write the selection record to `okf/testing/<slice>.md` in the format the taxonomy specifies, including every rejection and its reason.

Done when: all twenty categories carry a verdict. A category silently omitted is indistinguishable from a hole — that is the failure mode this step exists to prevent.

### 3. Cartograph

Spawn one `test-cartographer` on the slice, passing the coverage output. Wait for it.

Done when: every uncovered line in the slice carries a verdict, and the live ones are split into work units. If the map has gaps, send it back — do not start authoring from a partial map.

### 4. Author, in parallel — blind

Spawn one `test-author` per work unit, **all in a single message** so they run concurrently. Give each: its unit's files, its live items with the production events the cartographer named, the slice path, **the categories step 2 selected for those files with their done-conditions quoted**, and **two named reference test files** to match conventions against — or an explicit "this slice has no tests yet" if it does not.

The agent already carries its own blindness rules and the harness contract. Do not re-paste either. Do tell it anything specific to *this* slice's harness that the agent file does not cover.

Blindness is not ceremony. A prose-guided author reproduces the blind spots of whoever wrote the prose — which is how a slice ended up with a green suite that never exercised two of its fourteen event types.

Done when: every work unit reports back, every live item is either mapped to a named test or listed under `NOT COVERED` with a reason, every selected category for that unit has its done-condition met or is escalated as deferred, and every file carries a `RED PROOF`. A dropped live item is a failed run, not a rounding error.

### 4b. Resolve every finding, and verify it yourself

Do not relay an author's finding as fact. Read the source and confirm it before acting — and where a fix depends on how a value is consumed, read the consumer too.

For each confirmed defect **inside this slice**, either fix the source or mark the test `it.failing()`.

For each confirmed defect **outside this slice** — another slice, or the Go backend — **record it, do not fix it.** Verify it fully: read the other side, and write the contract test that proves it, marked `it.failing()` if it is genuinely broken. But the edit belongs to `/qa-integrate`, which sees every batch member's findings at once. A concurrent run may be inside that same file.

Reject an `it.failing()` that demands hardening nobody has committed to — it can never be promoted, and a permanently-red marker is noise the next person deletes.

### 5. Assassinate

Spawn `test-assassin` **one file at a time** — never two at once. Include the cartographer's **vacuous suspects** as extra targets; pre-existing tests get audited in the same pass.

Done when: every assassin reports `TREE CLEAN: yes`. Verify yourself with `git diff --stat` — no source file outside `__tests__/` or `*_test.go` may show changes. If one does, revert it before continuing.

### 6. Repair

For each survivor, spawn `test-author` again to strengthen the test that let it through, then re-run the assassin on that file only.

Loop until zero survivors — or until a survivor is genuinely a source defect rather than a weak test, in which case leave it and carry it into the record as a bug.

Do not accept a survivor because "the test is close enough." That is exactly the failure this skill exists to catch.

### 7. Record, measure, commit

Run the full slice suite, `npx tsc --noEmit` (mobile) or `go vet ./...` (Go), and a **scoped** mutation measurement — `npx stryker run --mutate "src/<path>/**/*.ts" --mutate "!src/<path>/**/__tests__/**"`. Measure `.ts` and `.tsx` separately if the slice has components; `/qa-integrate` needs both numbers to decide the glob.

**Do not touch `stryker.config.json`, `jest.config.js`, or `okf/testing/programme.md`.** Those three are the only files a concurrent run also wants, and every gate move happens once, at integration, where the combined score can actually be measured. Your numbers go in your own record.

Finish `okf/testing/<slice>.md` with a block `/qa-integrate` reads verbatim:

```
FOR INTEGRATION
coverage floor (per-file verified): <stmts>/<branch>/<func>/<lines>
mutation (.ts only): <score>% over <n> mutants
mutation (.tsx): <score>% over <n> mutants — or "no components"
glob recommendation: <join .ts-only | join fully | stay out — and why>
cross-slice defects: <each, with the slice it belongs to>
dead code (proposed, not deleted): <file>:<lines>
left dark (deliberate): <file>:<lines> — <the invariant that makes it unreachable>
```

Commit to the branch with a Conventional Commit. Do **not** merge, and do **not** remove the worktree — `/qa-integrate` needs both. Then report:

```
SLICE: <path>          BRANCH: <name>
COVERAGE: <before>% → <after>%
TESTS ADDED: <n> across <n> files
KILL RATE: <killed>/<applied> mutations
TAXONOMY: <n> selected — <n> satisfied, <n> deferred; <n> rejected with reason
SURVIVORS: <n> found, <n> killed, <n> left as source bugs
BUGS FIXED (this slice): <list>
RECORDED FOR INTEGRATION: <cross-slice defects, dead code, gate recommendation>
```

Never delete dead code as part of this run. Surfacing it is the deliverable; removing it is the user's call.

## Constraints

- **Never run `go test -race` locally** — Windows, no C toolchain. CI runs it on Linux.
- Never run `fallow` from the main tree while a worktree exists — it scans `.claude/worktrees/`.
- `AIDEV-NOTE` / `DECISION` / `WARNING` anchors are durable; nothing here strips them.
- `okf/` concepts have a pre-commit staleness hook. If this run changes behavior a concept describes, update the concept in the same commit.
- Commits are Conventional Commits, scope from `commitlint.config.js`, and carry no Claude co-author trailer.
