# Workflow: new feature

The disciplined loop for adding a feature. Each step has a backing skill that auto-fires on context.

```
0. /common-ground            → surface assumptions before scoping
1. /feature-spec <name>      → docs/specs/<name>/spec.md (+ spec-reviewer clarify-gate)
2. /feature-plan <name>      → docs/specs/<name>/plan.md (+ plan-reviewer)
3. EnterPlanMode             → final go/no-go on the plan
4. Per slice (loop):
   /tdd-red-green-refactor   → failing test → minimum impl → green → refactor
5. /verify-end-to-end        → typecheck · lint · unit · integration · slice-affecting e2e
   ↳ /run                    → launch the app, confirm the slice renders/runs (catches crashes/blank screens TDD misses)
6. /code-review-6-aspect     → 6 parallel subagents on the diff
7. (resolve findings; back to step 5 if needed)
8. /update-nested-claude-md  → every touched feature / context dir has a fresh CLAUDE.md (/git-commit enforced)
9. /adr-write                → if architectural decisions emerged
10. /compound-learning       → if patterns / mistakes worth capturing
11. /git-commit              → Conventional Commits, no AI co-author
```

## Step-by-step

### 0. /common-ground (optional, 30 sec)

If you've been away from the project for >1 day, start with `/common-ground` to surface what Claude currently believes about the state. Correct anything wrong before scoping.

### 1. /feature-spec <name>

Picks a kebab-case name. Walks user through framing. Writes `docs/specs/<name>/spec.md` from `docs/specs/_template/spec.md`.

The skill **queries the software-architecture-design vault MCP first** to surface relevant patterns + trade-offs (per `.claude/rules/vault-consultation.md`).

After draft: `spec-reviewer` subagent runs the clarify-gate. Resolve blocking findings before proceeding.

Spec includes: Problem · User value · **Scope tier (MVP cut)** · Acceptance criteria · Out of scope · Design considerations · Dependencies · Risks · Telemetry.

The **Scope tier** forces a right-sizing decision: for this solo, pre-launch app, default to the *minimal* version that delivers user value and defer infra (caching, multi-provider, dedup, circuit breakers, telemetry alerts) to post-launch unless there's a concrete "needed now because …". This is the single biggest lever on total feature time — it keeps a feature from ballooning into a multi-week build before the app has users.

### 2. /feature-plan <name>

Reads the spec. Decomposes into **vertical slices**, 2–5 minutes each, with file paths + failing-test-first + verify command per slice.

The skill **queries the vault** for relevant patterns; lifts anti-patterns into the plan's Risks section.

`plan-reviewer` subagent grades slice quality. Resolve blocking findings.

**Lighter lane:** for a *small* feature (minimal tier, <~10 acceptance criteria, no new bounded context or external dependency), skip the `plan-reviewer` gate — decompose into slices inline and go straight to TDD. Reserve the full plan-reviewer roundtrip for genuinely complex features (multi-context, new dependency, >~10 ACs).

### 3. EnterPlanMode

Final user sign-off on the plan. Approve, revise, or reject. Approval → Claude exits plan mode and starts step 4.

### 4. Per-slice TDD

For each slice in order:

1. **RED**: write the failing test (`test-author` subagent can be dispatched if useful). Run it. Confirm fail. Commit `test(<scope>): add failing test for <behavior>`.
2. **GREEN**: write minimum code to pass. Run. Confirm pass. Commit `feat(<scope>): <summary>`.
3. **REFACTOR**: improve without breaking. Re-run. Commit `refactor(<scope>): <improvement>` (only if meaningful).

The `pre-tool-tdd-guard` hook blocks writes to production code in `src/` if no companion test exists (override with `[ALLOW-NO-TEST: <reason>]` for non-behavioral edits).

### 5. /verify-end-to-end

Runs the layered verification stack: typecheck → lint → unit → integration → e2e (scoped to affected slice). Reports actual output per phase. Never claims "passed" without showing output.

If anything fails, fix and re-run. Do not proceed.

### 5.5 /run (launch the app)

Tests being green is **not** the same as the app working. TDD verifies units and integration in isolation; it does not catch wiring, imports, runtime, or render errors — exactly the "didn't run / blank screen" failures that otherwise only surface when you open the app manually.

Before declaring the slice/feature done, run `/run` to launch the app and confirm the slice actually renders and behaves. One command — not a new review pass. If it crashes or shows nothing, fix before code review.

### 6. /code-review-6-aspect

Dispatches 6 parallel subagents (architecture, security, perf, testing, quality, docs). Each grades against the software-architecture-design vault. Findings merged + deduped + grouped by severity.

If 🚨 Block items exist: address them, re-run from step 5.

### 7. Iterate steps 5–6 until clean.

### 8. /update-nested-claude-md (auto-enforced)

Every feature/context dir the work touched must have a present and fresh `CLAUDE.md` before committing. The `/git-commit` skill's pre-commit checks enforce this — they check each touched dir for missing or stale CLAUDE.md and run `/update-nested-claude-md <dir>` before proceeding.

For each flagged path: mobile feature → AUTO-MAINTAINED block regen; backend bounded-context → hand-written sections from classes / Protocols / `# AIDEV-*` anchors. Commit with `docs(claude-md): ...`.

Override only when justified: add `[ALLOW-CLAUDE-MD-DRIFT: <reason>]` to the commit body. The override is logged to `.claude/claude-md-drift.log`.

### 9. /adr-write (if applicable)

If the feature involved architectural decisions (new pattern, library, layer boundary change), draft an ADR. User reviews, approves, commits.

### 10. /compound-learning (if applicable)

If something surprised you that future-you would also hit, capture it in `docs/solutions/`. Routine work doesn't need an entry — skip silently.

### 11. /git-commit (or commit per slice)

Conventional Commits with proper scope. The `.husky/commit-msg` hook strips AI attribution; commitlint validates format.

If commits piled up during the feature (one per slice), there's nothing to do here. If you held off commits, write them now with proper structure.

## Doc-freshness automation

After commits:
- `post-tool-commit-doc-drift` hook checks if changed code touched expected docs (spec for changed feature, glossary for new domain terms). Warns; never blocks. Override via `[ALLOW-DRIFT: <reason>]` in commit body.
- `/git-commit` skill pre-commit checks scan staged domain files for class names not in `docs/ubiquitous-language.md` and check touched feature/context dirs for missing or stale `CLAUDE.md`. Override CLAUDE.md hygiene via `[ALLOW-CLAUDE-MD-DRIFT: <reason>]` in commit body; logged to `.claude/claude-md-drift.log`.
- `/update-nested-claude-md` skill regenerates feature `CLAUDE.md` auto-maintained block (mobile) or refreshes hand-written backend bounded-context CLAUDE.md. Invoked by `/git-commit` pre-commit checks when missing/stale. Auto-commits.

## When the loop is overkill

- **Typo fixes / pure docs / dependency bumps** — skip spec/plan/TDD; commit directly.
- **Exploratory prototype** — use `docs/brainstorms/` instead of `docs/specs/`. When the prototype graduates, write a real spec.
- **Bug fixes** — see `docs/workflows/bug-fix.md` (lighter variant: reproduce-with-test → fix → green).

## When the loop is insufficient

- **Cross-feature refactor** — see `docs/workflows/refactor.md`.
- **Adopting a new library / framework / pattern** — `/brainstorm-tech-choice` first; produces an ADR before any feature work.
