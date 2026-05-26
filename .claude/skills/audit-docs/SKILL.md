---
name: audit-docs
description: |
  Fires when user says "audit the docs", "stale docs", "clean up brainstorms", "doc hygiene", or
  quarterly via a SessionStart-checked timestamp. Sweeps docs/specs/, docs/adr/, docs/solutions/,
  docs/brainstorms/ for staleness and consolidation opportunities; prompts for pruning/merging.
when_to_use: |
  Run quarterly or when documentation feels noisy. Not a regular per-feature skill.
---

# Audit docs

## What this skill does

A hygiene sweep across the doc tree. Categorizes every doc as **fresh / stale / candidate-for-pruning / consolidation-opportunity**. Asks user for action on each non-fresh entry.

## The sweep

### docs/brainstorms/ — TTL: 30 days untouched + not graduated

```
For each .md in docs/brainstorms/:
  age = days since last modification
  graduated = does a docs/specs/<x>/ or docs/adr/<y>.md cross-reference this file?
  if age > 30 and not graduated:
    candidate for pruning
```

Present list. For each:
- `[archive]` — move to `docs/notes/archived-brainstorms/` (keeps history, signals "no longer active")
- `[delete]` — git rm
- `[keep]` — move to `docs/notes/` (escapes TTL)
- `[graduate]` — move into a new `docs/specs/<x>/` or `docs/adr/`

### docs/solutions/ — consolidation opportunity

Group entries by tag. If 3+ entries on the same tag in the last 6 months, propose consolidation into a single distilled doc.

Present per cluster:
- The N source files
- Proposed consolidated title + sections
- Source files get archived (`docs/notes/archived-solutions/`) with a redirect note pointing at the new consolidated doc

### docs/specs/ — abandonment detection

```
For each docs/specs/<feat>/:
  has plan? has corresponding code in apps/mobile/src/features/<feat>/ or services/api/?
  if spec only (no plan, no code): "abandoned spec" — confirm and archive or revive
```

### docs/adr/ — supersede chains

Check each ADR's status:
- `Accepted` ADRs with no current code reference: flag for review (maybe Superseded?)
- `Deprecated` / `Superseded` ADRs: confirm the superseder is linked

### Root + nested CLAUDE.md — bloat check

For each CLAUDE.md, line count:
- root CLAUDE.md > 60 lines: warn (target ≤60)
- nested CLAUDE.md > 30 lines: warn (target ≤30, promote to `.claude/rules/`)
- Any CLAUDE.md > 200 lines: hard warn — Boris's ceiling, Claude starts ignoring

## Report shape

```markdown
# Docs audit — YYYY-MM-DD

## Brainstorms (N stale)
- docs/brainstorms/2026-01-05-auth-spike.md — 130 days, not graduated
  Action: [archive | delete | keep | graduate]
- ...

## Solutions (N consolidation opportunities)
- Cluster "async-pitfalls" (4 entries): propose consolidation into docs/solutions/2026-MM-DD-async-pitfalls.md
- ...

## Specs (N abandoned)
- docs/specs/voice-control/ — spec only, no plan/code in 90 days
  Action: [archive | revive]

## ADRs (N to review)
- ADR-0007 — Accepted, but code no longer references the chosen library
  Action: [mark superseded | confirm still in use]

## CLAUDE.md bloat (N over budget)
- apps/mobile/src/features/library/CLAUDE.md — 45 lines (target 30)
  Action: split / promote sections to .claude/rules/
```

## Commit

After user takes actions, commit per category:
- `chore(brainstorms): archive 5 stale entries`
- `docs(solutions): consolidate async-pitfalls cluster (5→1)`
- `chore(specs): archive abandoned voice-control spec`

## Anti-patterns

- Bulk-delete without asking.
- Consolidating entries that aren't actually related (forced merging).
- Treating "old" as "obsolete" — an ADR from year 1 may still govern the project.
- Skipping the audit because "nothing seems stale" — run it on schedule; humans miss creep.
