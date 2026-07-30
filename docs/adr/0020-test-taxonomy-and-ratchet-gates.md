# ADR-0020: Test taxonomy, blind rebuild, and ratchet gates

- **Status:** Accepted
- **Date:** 2026-07-30
- **Deciders:** solo + Claude
- **Context tags:** [testing, quality-gates, mutation-testing, ci]

## Context

The starting question was whether Robert Martin's position — *"I do not read the code my agents write; instead I surround them with extreme constraints: unit tests, gherkin tests, QA procedures, quality metrics, mutation testing, test coverage"* — is workable here. Not reading agent-written code is only defensible if the constraints are strong enough that passing them *means* something. So the honest first move was to measure the constraints rather than to add more.

An agent-driven mutation pass on 2026-07-29 ran 72 mutations against six `apps/mobile/src/shared/` subsystems, under a suite of **552 passing tests**. It killed 26. **A 38% kill rate**: roughly two-thirds of what the suite executed, it did not constrain. Coverage counted every survivor's line as covered.

The suite was not careless — zero snapshot tests, dense assertions, and one genuinely excellent cross-surface contract test. Three pieces of *infrastructure* were hiding untested code:

- Module-wide `jest.mock` of a whole file removed it from execution entirely, while coverage still reported the module.
- The `expo-file-system` double had `write() {}`, unwired from `textSync()`. **No test could observe persistence at all**, so both of the app's durability guarantees (offline pins, critical telemetry) were unassertable. Gutting `pinnedStore.saveIndex` and inverting `outboxStore.persistOutbox` each left the suite green.
- A single global `coverageThreshold` blended per-file zeros into one average nobody inspected.

Meanwhile nothing in CI ran the mobile suite at all, and neither shipping path ran any check: `deploy-backend.yml` raced its own test workflow on the same push, and a `v*` tag matched no test trigger, so every `.ipa` shipped unverified.

The deeper problem was that no artifact answered *which constraints a piece of code deserves*. Coverage answers "did this line run." Nothing asked "is this the right set of tests, and do they hold."

## Decision

**1. A twenty-category test taxonomy** (`okf/playbooks/test-taxonomy.md`), in six families, each category carrying the code property that triggers it, what it proves, and a checkable done-condition. Applicability is *derived* from a property of the code, not from judgment about what feels worth testing. Every category cites the concrete survivor or written rule that justifies it, so the list is derived from this codebase rather than imported.

**2. Per-slice selection records** committed under `okf/testing/`, recording every category as selected, **rejected with a reason**, or deferred. A reasoned rejection is a finding; a silently omitted category is indistinguishable from a hole — which is exactly how a slice kept a green suite that never exercised two of its fourteen event types.

**3. Rebuild blind.** Authors receive the source and the taxonomy only, and are barred from `okf/` (except the taxonomy), from recovering deleted tests via git, and from any `CLAUDE.md` test list. A prose-guided author reproduces the blind spots of whoever wrote the prose. Blind authors are also required to report source defects without fixing them.

**4. Ratchets, not bars.** Per-glob coverage floors, a Stryker mutation-score floor, and count ceilings for static analysis — all raise-only. This is what makes an imperfect codebase gateable today instead of after a stop-the-world cleanup.

**5. Both mutation mechanisms, for different jobs.** Stryker gives a reproducible number a ratchet can stand on. `test-assassin` gives the semantic mutations no generator produces — a swapped adjacent field, a reversed spread so the stale copy wins, a commit before its send — which found the worst defects in the audit.

**6. A test asserts intended behaviour, never a defect.** On finding a bug: fix the source, or mark the test `it.failing()`. A passing test that encodes broken behaviour protects the defect.

## Consequences

The mobile suite was deleted (90 files, 7,740 lines) and is being rebuilt slice by slice. `shared/events` went from a **0/9 kill rate to 90.75%** and surfaced nine defects, one a live cross-surface break where the client read a field the Go publisher has never sent.

Two findings generalise beyond this repo:

**Mutation testing and blind derivation find disjoint defect classes.** Mutation asks *"would the suite notice this change?"* and so finds weak tests over correct code. Blind derivation asks *"is this right?"* and so finds wrong code. The `failure_message` defect proves they cannot substitute: the field is never sent, so replacing its read with `null` is an *equivalent* mutation that no runner can surface. Both are required.

**Verify that a new gate fails when it should.** Four separate things looked like gates and were not: react-doctor ran advisory; the filesystem double could not round-trip; `fallow dead-code` exits 0 even with an `error`-severity finding, so `--fail-on-issues` is what actually gates; and `tsc` was *stronger locally than in CI*, because expo-router's generated types are gitignored. Each was invisible until forced to run for real. A gate is not proven by passing.

Costs accepted: ~24 documentation files touched per slice deletion because of the `okf/`+`CLAUDE.md` staleness hooks; a doubled backend test run on pushes to `main`, so `test-backend.yml` stays the independent record that main is green; and a mutation run measured in minutes, which is why it is nightly rather than per-PR.

## Alternatives rejected

**Trust the suite and stop reading the code.** The original proposition. Rejected because 38% is not a base to stop reading over. The stance becomes defensible only once the done-conditions hold and the kill rate is high — it is an *outcome* of the programme, not its premise.

**Keep the existing suite and add to it.** Tempting, and I argued for it: the 26 kills were real constraints and the incident-regression tests encoded production bugs. Rejected on the owner's call, and the risk was mitigated by harvesting: the findings and rationale largely already lived in `okf/`, and the rest were written into `okf/testing/` before deletion. The blind rebuild also turned out to be *better* than an additive pass, because it found nine defects an author reading the old tests would have inherited blind spots about.

**Refactor for testability first.** The instinct was that code shape was the obstacle — mixed logic in single functions, insufficient decomposition. The mutation data refuted it: every survivor sat in a function already small and pure enough to test exhaustively. `if (prev >= 0)` is as simple as a condition gets and it survived, and extracting it to `canStepBack()` would not have killed the mutant — only an input at `currentIndex === 1` does. Decomposition changes where logic lives, not which cases are exercised. Extraction is now justified only where it changes *reachability* (a derivation trapped inside a component), never to raise a count. Refactoring under a suite constraining 38% would also have meant restructuring code with a net full of holes.

**Optimise lines of test code.** Rejected because it selects against the strongest techniques: one `fast-check` property is a handful of lines and constrains an unbounded input space, where the equivalent hand-written table is ten times longer and covers only the rows someone thought of. The meters are kill rate and categories-satisfied.

**Chase 100% mutation score.** Rejected. Of 56 survivors in the first `shared/events` run, roughly eight named real defects; the rest were guard-clause conditionals and error-message strings, and this repo's own convention forbids asserting error copy, making those equivalent by rule. Triage and record, do not chase — an unexplained number is the coverage trap in a new costume.

**Visual-regression / screenshot diffing.** Rejected as a category, documented in the taxonomy so it stays rejected. Its failure mode matches snapshot tests: when the baseline breaks the cheap fix is to regenerate it, which under agent authorship is what happens.

**Clear the static-analysis backlog before continuing.** 137 fallow complexity findings and 106 react-doctor warnings. Rejected: they are already ratcheted so they can only fall, they span slices whose code will be restructured as those slices get tested, and acting on "split this high-impact file" before that slice has tests is the refactor-first mistake again.
