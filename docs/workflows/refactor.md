# Workflow: refactor

Code structure improvement with **no behavior change**. Discipline is around safety, not novelty.

```
1. Confirm tests cover the area (add tests first if not)
2. /brainstorm-tech-choice    → if the refactor changes architecture (otherwise skip)
3. /adr-write                  → if architecture changes
4. /feature-plan               → for multi-step refactors
5. Per slice:
   - Refactor
   - /verify-end-to-end        → confirm tests stay green
6. /code-review-6-aspect       → especially architecture + quality lenses
7. /compound-learning          → capture the "why we refactored" pattern
8. /git-commit                 → refactor(<scope>): ...
```

## The rule

**Refactoring requires green tests before AND after.** If tests aren't green to start, you're not refactoring — you're rewriting.

Before any refactor:
1. `/verify-end-to-end` on the affected area. Confirm green.
2. If coverage is thin, **add tests first** (as a separate commit) before touching code. The tests are the safety net.

## What's a refactor (vs. feature work)

| Activity | Refactor? |
|---|---|
| Renaming a function for clarity | Yes |
| Splitting a class for SRP | Yes |
| Extracting a Strategy from an if-chain | Yes |
| Replacing one library with another (same surface) | Yes |
| Adding a feature | No — `/feature-spec` |
| Changing API surface | No — `/feature-spec` |
| Fixing a bug | No — `/bug-fix` (which incidentally may clean up code) |
| Performance optimization that changes behavior | No — feature with ADR |

If the change adds or changes behavior, it's not a refactor. Use the right workflow.

## Multi-step refactors

When the refactor spans >3 files or >100 lines:

1. Write a **plan** (`/feature-plan` works for refactors too) — break into commits-per-step, each leaving the code in a working state.
2. Each step ends with green tests.
3. Each step is committable on its own (in case you need to stop mid-way).

The plan-reviewer subagent validates the slicing.

## Architectural refactors

When the refactor changes a boundary (e.g., extracting a bounded context, changing the persistence pattern):

1. `/brainstorm-tech-choice` to evaluate the destination architecture.
2. `/adr-write` for the decision.
3. Plan as multi-step. Often involves a strangler-fig pattern — `[vault: wiki/concepts/Strangler Fig Pattern.md]`.
4. Don't merge the partial refactor — finish it or revert it. Half-refactored code is technical debt that compounds.

## Anti-patterns

- **Refactor mixed with behavior change** in one commit. Reviewers can't separate intent. Split into refactor-commit + change-commit.
- **"While I'm here" refactors** — touching unrelated code during feature/bug work. Violates surgical-changes (Karpathy). Note it as a TODO, separate refactor later.
- **Refactor without tests** — you don't know if you broke anything. Don't.
- **Big-bang rewrites disguised as refactors** — if the change is "rewrite X completely," that's a feature/migration. ADR + plan it explicitly.

## Commit shape

```
refactor(<scope>): <what changed structurally>

<why this improves the code — usually citing a SOLID / KISS / DRY rationale or
naming a [vault: ...] pattern being adopted>

No behavior change. Tests green before and after (see CI / verify output).
```

## Migration sub-workflow

For DB schema migrations specifically: `.claude/rules/migrations.md` applies. Shipped migrations are immutable. New schema changes are **always** new migration files.
