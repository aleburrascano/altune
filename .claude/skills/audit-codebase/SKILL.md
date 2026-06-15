---
name: audit-codebase
description: >
  Sweep the codebase against all rules — frontend, backend, or both.
  Reads every file so path-scoped rules load automatically, then audits
  against them. Reports findings by severity, optionally fixes them.
  Use every 3-4 days or after shipping a feature batch.
argument-hint: "[frontend | backend | all] [--fix]"
---

# Codebase Audit

Systematic sweep of source files against all project rules. Reading source files triggers the relevant path-scoped rules to load — they define what's right and wrong.

## Arguments

- `frontend` — sweep `apps/mobile/src/`
- `backend` — sweep `services/go-api/`
- `all` — both (default if no argument)
- `--fix` — apply fixes after reporting (default: report only)

## Workflow

### Phase 1: Discovery

Enumerate every source file in the target scope. Group by area:

**Frontend**: each feature folder, shared/, app/
**Backend**: each bounded context in internal/, shared/, cmd/

Report file count per area before starting.

### Phase 2: Audit

Process one area at a time. For each area, read EVERY file — no skipping.

As files are read, path-scoped rules load automatically. Apply every loaded rule to each file. Think about cross-file issues that only appear when you see multiple files together — duplication, inconsistent patterns, missing abstractions, architecture violations.

### Phase 3: Report

Group findings by severity:

- **Critical** — bugs, security issues, data loss risks
- **High** — architecture violations, missing resilience, significant quality gaps
- **Medium** — convention violations, missing tests, abstraction opportunities
- **Low** — style improvements, minor optimizations

For each finding: file path, what's wrong, which rule it violates, concrete suggested fix.

End with a summary table (area x severity x file count).

### Phase 4: Fix (only if `--fix`)

Work through findings critical-first. Commit fixes per area.

## Context management

Complete one area fully before moving to the next. Ask the user before continuing — this allows context to reset between areas if needed.
