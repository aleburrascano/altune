---
name: qa-integrate
description: Merge a batch of /qa-slice branches, apply every ratchet in one pass, re-measure the combined mutation score, and write the ledger.
disable-model-invocation: true
---

# Integrate a QA batch

`/qa-slice` runs are deliberately isolated: each works in its own worktree and **no run touches a shared gate**. This skill is where the gates actually move. Run it in the main tree, after a batch finishes.

It is required even for a batch of one. The combined mutation measurement and the per-file floor verification were always serial — isolating them is what lets the slices themselves fan out.

## What cannot be composed

**The combined mutation score is not the average of its parts, and not any function of them.** It is a ratio over the union of mutants, and tests in one slice kill mutants in another. A harness tightening in `shared/telemetry` took `shared/offline` from 92.71% to 93.12% with no test written for it. Two branches each reporting a number tell you nothing about the merged number. **Measure it once, after the merge, before committing the threshold.**

**A per-glob coverage floor is enforced per *file*.** A key like `'src/shared/events/**'` makes Jest apply the threshold to every matching file individually, not to their aggregate. Slice 1 committed 97/90/100/100 as though it were an aggregate; two of its own files sit below it, and `npx jest --coverage --ci` has been red ever since. Verify every floor per-file before you commit it.

**Raise-only, without exception.** If a number will not clear, that is a finding for the user, never a reason to lower the committed one.

## Steps

### 1. Collect

Find the batch: `git branch --list 'qa/*'` and `git worktree list`. For each branch, read its `okf/testing/<slice>.md` — specifically the `FOR INTEGRATION` block `/qa-slice` step 7 wrote.

Done when: every branch has a record with a `FOR INTEGRATION` block. A branch without one did not finish — say so and stop rather than merging a partial run.

### 2. Merge

Merge each branch into a single integration branch, one at a time.

Test files and per-slice records are disjoint by construction. Real conflicts are only possible in three places, and `/qa-slice` is forbidden from editing all three — `stryker.config.json`, `jest.config.js`, and `okf/testing/programme.md`. **A conflict in any of them means a run broke its contract.** Read what it did before resolving, and say so in the report.

Done when: the tree merges clean and `git status` is empty. **Stop and show the user** any conflict you did not resolve mechanically.

### 3. Verify the suite before touching a gate

Run, in the main tree:

```
cd apps/mobile && npx jest && npx tsc --noEmit
```

A gate moved over a red suite is worse than no gate. Done when both pass. If the merged suite fails where each branch passed, that is a genuine cross-slice interaction — the most valuable thing this step finds. Report it and stop.

### 4. Apply the coverage ratchets

For each slice, take the floor from its record and **verify it per file** before committing:

```
npx jest --coverage --ci
```

Read the per-file table, not the summary. A floor is committable only if every file it covers holds it. If a file falls short, the choices are: bring that file up, or split the ratchet into per-file entries recording what each file actually holds. **Never lower a committed number to fit.**

Done when `npx jest --coverage --ci` — the exact command in `test-mobile.yml` — exits 0, or the user has decided how to handle a file that will not clear.

Note the known standing breach: `sse-client.ts` and `trackCachePatch.ts` sit under slice 1's committed floor and have since it landed. If that is still the only failure, it is pre-existing — say so and do not let it block the batch.

### 5. Decide the mutate glob

Each record carries a `glob recommendation` and separate `.ts` / `.tsx` numbers.

A slice with no components joins the glob on the existing `.ts`-only terms. A slice **with** components does not have a precedent — every slice so far has been `.ts`-only, which excluded its components without anyone deciding anything, and the threshold has been raised seven times on that basis. `shared/acquisition` sits out of the glob entirely because `StyleSheet`/`Animated` mutants in two presentational components drag it to 79.44%, and this repo's convention says not to chase those.

**Stop and show the user** the first time a component-bearing slice wants in. Give them its `.ts`-only score, its `.tsx` score, and what each does to the combined number. The options are a `.tsx` exclusion in the glob, a separate threshold, or per-slice configs. Their call — it sets the precedent for every feature slice after it.

### 6. Measure the combined score

Update the `mutate` glob, then run the **full** measurement:

```
cd apps/mobile && npm run mutate
```

Raise `thresholds.break` to the highest integer the measured score clears, and no higher. If the score is 93.79, `break` stays at 93 — 94 does not clear. State it that way in the ledger; that is how every prior entry reads.

Done when the measured combined score is in hand and `break` is set from it. **Never infer this number.** Committing a config change whose gate you have not run is how a red CI gate gets discovered by someone else.

### 7. Triage the cross-slice findings

Pool every `cross-slice defects` entry from every record. These are defects a slice verified but was forbidden to fix, because the fix lands in another slice or on the backend.

For each: confirm it against the source yourself, then either apply the fix here — where you can see the whole batch at once — or add it to `okf/testing/programme.md`'s **Outstanding** section with the slice it belongs to.

Fixing one means amending that slice's record too. **Stop and show the user** the pooled list before applying anything; a fix outside the batch's slices is their call, not yours.

Done when every pooled finding is either fixed, or written into Outstanding with its owning slice named.

### 8. Write the ledger

Update `okf/testing/programme.md`:

- one ledger row per slice — status, kill rate, coverage floor
- the running combined score and what `break` moved to, in the existing prose style
- new Outstanding entries; delete any entry this batch cleared

Then commit. Conventional Commits, scope from `commitlint.config.js`, no Claude co-author trailer. The `okf/` staleness hook will want the concepts this batch's source fixes touched — update them in the same commit.

### 9. Report

```
INTEGRATED: <n> slices — <paths>

MERGE:      clean | <n> conflicts resolved, <n> escalated
SUITE:      <pass/fail> — <n> tests
TYPECHECK:  <pass/fail>
COVERAGE:   per-file verified — <n> files below their committed floor
MUTATION:   combined <before>% → <after>% over <n> mutants
            break <before> → <after>   (or: stays at <n> — <n+1> does not clear)

PER SLICE
  <slice>   <cov> → <cov>   <kill>%   <n> tests   <n> defects fixed

NEEDS YOU
- <each decision escalated, with what it would take>

CROSS-SLICE FINDINGS
- <fixed here> / <written to Outstanding, owner named>

LEDGER: <n> rows written, <n> Outstanding added, <n> cleared
WORKTREES: kept at <paths>
```

## Teardown

**Do not remove a worktree you just merged.** Print the command and let the user run it once they are satisfied.

The junctioned `node_modules` must be unlinked before the directory is removed. `Remove-Item -Recurse` in PowerShell 5.1 follows junctions and will delete the real packages out of the main tree:

```powershell
$wt = ".claude/worktrees/<name>"
Get-ChildItem "$wt/apps/mobile/node_modules" -Force |
  Where-Object { $_.Attributes -band [IO.FileAttributes]::ReparsePoint } |
  ForEach-Object { & cmd /c rmdir "`"$($_.FullName)`"" }
Remove-Item $wt -Recurse -Force
git worktree prune
```

Verify afterwards: `(Get-ChildItem apps/mobile/node_modules -Force).Count` must be unchanged.
