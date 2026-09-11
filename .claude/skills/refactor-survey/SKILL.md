---
name: refactor-survey
description: Survey ONE module for behavior-preserving refactors and emit curated, banded GitHub tickets. Read-only — the tickets are the output, never code. Use per module when clearing refactor debt before features. Trigger words: refactor-survey, survey a module for refactors, refactor tickets for <module>, refactor debt.
---

# Refactor survey

One module in, curated refactor tickets out. **Read-only**: survey → curate → emit. This skill never edits code — the GitHub tickets are the whole output.

## Input

A single module path. Examples:
- `services/go-api/internal/discovery`, `services/go-api/internal/catalog`
- `apps/mobile/src/shared`, `apps/mobile/src/features`

**One module per run.** If none is named, ask which; do not survey the whole repo at once.

## 1. Survey — the lens (read-only)

Detect mechanically, judge with the model. **Find candidates by grepping the module and counting** — the checklist below is fixed, so every module is asked the identical questions, which is what makes runs comparable. For a large module, fan out read-only sub-agents over disjoint sub-areas so no two touch the same files. (A maintainability/evolvability rubric under `~/.claude/skills/codebase-workflow/domains/` is a useful reference if it exists, but the checklist here stands alone.)

Hunt for **cost** — where the current structure makes change expensive, risky, or confusing:

- **Duplication past Rule of Three** — the same shape written 3+ times *and drifting*. Count the copies.
- **God object / long function / file** — many reasons to change; dependency-count and line-count outliers.
- **Missing or leaky boundary** — a concern scattered with no owner (a would-be module), or a layer importing what it must not.
- **Dead code** — a symbol/function with zero non-test callers.
- **Primitive obsession** — a raw string/int where a value object or enum belongs.
- **Change-amplification** — one small change forces edits in N files; missing one is a silent bug.
- **Drift** — the same thing done two different ways.

**Counter-lens — do NOT flag unless all hold:**

- The copies drift, or there are 3+ (earn the abstraction; a *new* abstraction needs a concrete second case that exists today).
- You can name the do-nothing alternative AND when do-nothing wins. If do-nothing wins, it is **SOUND**, not a finding.
- The change is **behavior-preserving** — no features, no bug fixes.

**Every finding must cite a count** — "N copies at these paths:lines", "0 non-test callers", "an X-line function with Y responsibilities". A count is a checkable fact that reproduces across runs; a finding that cannot cite one does not ship. This — not any tool — is what keeps the survey consistent from run to run and module to module.

If the repo already runs a duplication, dead-code, or complexity detector (peek at its CI, `package.json` scripts, or Makefile), fold its output in too — but **discover it, never depend on it**. The grep-count is the floor; the skill must work in a bare repo with nothing installed.

## 2. Curate

- **Drop** every `never` / low-value finding.
- **Bundle** tightly-related micro-items into one ticket (all dead code → one; a family of CLI/handler helpers → one).
- **Dedupe** against existing open refactor tickets: `gh issue list --label type:refactor --state open --limit 100`. Never re-create one.

Prefer **5 earned tickets over 20 makework ones.** Stop at earned findings; do not chase a module to zero.

## 3. Emit

For each surviving finding, `gh issue create`:

- **Title:** `[Refactor]: <the smell, one line>`
- **Labels:** `type:refactor` + the matching `area:*` and `platform:*` from `.github/labels.yml`. Add `ready` **only** when the band is `now`.
- **Body:** the template below.

Then report the created tickets AND the **SOUND / rejected** list — what you deliberately left and why — so the run is auditable.

### Ticket body template

```
## Smell
<evidence: files + line ranges + counts, and what it costs>

## Change
<the concrete refactor and its proposed home/structure>

**Trigger:** now | before-growth | when-it-hurts — <the trigger that makes it worth doing>

## Done when
- [ ] gate green (behavior unchanged)
- [ ] <the concrete structure outcome>
- [ ] every new abstraction names what it beat in the PR body (else delete it)
- [ ] `codebase-workflow` run on the diff — no new BROKEN/COSTLY, rejected cases listed
- [ ] nested CLAUDE.md updated if any file moved/renamed

## Out of scope
Behavior changes, adjacent modules, features. A bug found here is a separate ticket.

_Surfaced by refactor-survey of <module>._
```

## Guardrails

- One module per run. Never edit code in this skill — the tickets are the output.
- Only a `now` finding gets `ready`; everything else carries its trigger as backlog.
- Behavior-preserving refactors only. A bug or a feature is a different ticket type.
- **Re-running the same module is safe and convergent.** The dedup step means a second pass adds only what the first missed and never duplicates, so you get completeness over a couple of runs rather than from one perfect run — the answer to the non-determinism of any single pass.
- Hand off: once the `ready` tickets exist, the user runs `/crew` (in waves of non-overlapping files) to execute them.
