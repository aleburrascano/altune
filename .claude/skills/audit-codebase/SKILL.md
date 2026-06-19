---
name: audit-codebase
description: >
  Sweep the codebase against all rules — frontend, backend, or both.
  Reads every file so path-scoped rules load automatically, then audits
  against them. Reports findings by severity, surfaces improvement
  opportunities beyond rule compliance, and optionally fixes them.
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

### Phase 4: Opportunities

After the rule-based audit, step back and think about what's **missing** — not rule violations, but gaps a real user, tester, or ops engineer would notice. The audit catches what's *wrong*; this phase catches what's *absent*.

Think like a user who just installed the app, a QA tester trying to break it, and an on-call engineer debugging a 2 AM incident — all at once. For each area you just audited, ask:

- **What would a real user expect to happen here that doesn't?** Walk through every flow end-to-end. Where does the experience fall short of what a polished app would do?
- **What would break silently?** Where can state go stale, mutations go unconfirmed, errors go swallowed, or partial failures leave things in a bad state?
- **What's missing that would save hours of debugging?** What would you wish existed the first time something goes wrong in production?
- **What can regress without anyone noticing?** Where is a fix in one layer likely to break another, with no test or signal to catch it?

Don't limit yourself to a checklist. These questions are starting points — follow whatever threads they surface. The goal is to find things the rule-based audit can't catch because no rule exists for them yet.

For each opportunity:
- What's missing and where (be specific — name the screen, endpoint, or flow)
- Why it matters (user impact, debugging impact, regression risk)
- Suggested approach (1-2 sentences, not a full spec)
- Effort estimate: **quick** (< 30 min), **medium** (1-2 hours), **significant** (needs a spec)

Group opportunities by effort so the user can pick off quick wins first.

### Phase 5: Fix (only if `--fix`)

Work through findings critical-first, then quick-win opportunities. Commit fixes per area.

## Context management

Complete one area fully before moving to the next. Ask the user before continuing — this allows context to reset between areas if needed.
