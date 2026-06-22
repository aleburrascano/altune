---
name: audit-codebase
description: >
  Low-altitude conformance sweep of one area against the project's path-scoped
  rules and beyond them — reports what's wrong and what's worth improving,
  optionally fixes. Run per area every 3-4 days or after a feature batch.
  For structural/architecture work, use /improve-codebase-architecture.
disable-model-invocation: true
argument-hint: "<frontend | backend | area-name> [--fix]"
---

# Codebase Audit

A sweep of source files for the **substance** of the code — correctness, security, tests, resilience, readability. Two principles govern it.

## Altitude

This skill works at **low altitude**: lines, functions, files. *Is this code correct, secure, tested, resilient, clean?*

Structural questions — should this module be reshaped, is this seam in the right place, is this abstraction shallow — are **high altitude**. They belong to `/improve-codebase-architecture`. When a finding wants to *restructure* rather than *fix*, that's the handoff signal: name it and point there.

## Rules are the floor, not the ceiling

Reading a source file auto-loads the path-scoped rules that govern it. Those are the **floor** — cheap, certain, checkable. But the rules are limited; many real problems no rule names. So every finding is one of two kinds:

- **Rule-backed** — cite the rule (e.g. `go-resilience.md`, `rn-security.md`).
- **Judgment** — no rule exists, but it's still wrong or risky. You are expected to find these. Stopping at "rules applied" is an incomplete audit.

## Arguments

- `frontend` — sweep `apps/mobile/src/`
- `backend` — sweep `services/go-api/`
- an **area name** (a feature folder or bounded context) — sweep just that slice
- `--fix` — apply fixes after reporting (default: report only)

**One area per run.** A full-repo sweep won't fit one context. Pick a scope you can read completely.

## Workflow

### 1. Discovery

Enumerate every source file in the target area and report the count before starting. That count is the completion criterion for the next phase — every file accounted for, none skipped.

### 2. Sweep

Read every file in the area. Reading loads the path-scoped rules automatically. For each file — and across files, where a problem only shows when several are seen together — look across these dimensions:

- **Correctness** — logic errors, edge cases, nil/undefined, races, state bugs
- **Security** — unvalidated input, authz gaps, injection, leaked secrets
- **Error handling & resilience** — swallowed errors, missing timeouts/retries, partial-failure states left bad
- **Tests** — missing coverage, weak assertions, brittle implementation-coupled tests
- **Testability & decomposition** — functions that do more than one thing, or can't be tested without standing up their callers. Flag these **even when they pass the line-count rule** — `code-quality.md` (≤10 lines) is the floor; *hard to test* is the real signal.
- **Performance** — N+1 queries, blocking calls, needless allocation (line-level, not architectural)
- **Observability** — error sites with no log or correlation id
- **Cleanliness** — dead code, misleading names, local duplication

These are the common dimensions, not a closed list — follow anything that smells wrong, even if it fits none of them.

The sweep produces two outputs: **findings** (what's wrong → phase 3) and **suggestions** (what's worth improving → phase 4).

### 3. Findings — what's wrong

Group by severity:

- **Critical** — bugs, security holes, data-loss risks
- **High** — missing resilience, significant quality gaps, rule violations that bite
- **Medium** — convention drift, missing tests, weak decomposition
- **Low** — naming, minor cleanup

Per finding: file path · what's wrong · rule (if any) · concrete fix. End with a severity × file-count summary table.

### 4. Suggestions — what's worth improving

Not violations — concrete improvements a careful engineer would make. Keep them at **low altitude**: "add input validation to this endpoint," "this error is swallowed, surface it," "split this function so the error branch is testable." If a suggestion wants to *reshape a module* rather than fix a line, don't write it here — note it as architectural and point to `/improve-codebase-architecture`.

Think like three people at once — a user hitting the flow, a tester trying to break it, an on-call engineer at 2 AM:

- What would a user expect to happen here that doesn't?
- What breaks silently — stale state, unconfirmed mutations, swallowed partial failures?
- What's missing that would save hours when something breaks in production?

Per suggestion: what & where (name the screen, endpoint, or function) · why it matters · approach in 1-2 sentences · effort — **quick** (< 30 min) / **medium** (1-2 h) / **significant** (needs a spec). Group by effort so quick wins are pickable first.

### 5. Fix (only if `--fix`)

Work critical-first, then quick-win suggestions. Commit fixes per area.

## Context management

One run audits one area. To sweep another, start a fresh invocation so context resets cleanly between areas.
