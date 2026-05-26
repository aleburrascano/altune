---
name: compound-learning
description: |
  Fires from the Stop hook after non-trivial work (≥2 file edits, OR new file created, OR test added).
  ALSO fires explicitly when user says "capture what we learned", "lesson here", "compound this",
  "so the lesson is", "gotcha", "we should remember". Writes docs/solutions/YYYY-MM-DD-<slug>.md if a
  pattern (not bug instance) is worth recording. Skips silently if nothing surprising happened.
when_to_use: |
  Use at session end or after a debugging/refactor session. Skip if the session was routine boilerplate.
---

# Compound learning

## What this skill does

Reviews what happened in the session and decides if there's a **pattern** worth recording (not just a bug instance). Writes a markdown file in `docs/solutions/` that future sessions can find and apply.

## The decision: record or skip

Ask:
1. Did we hit a surprise that future-me would also hit?
2. Was there a non-obvious tradeoff we made that should be defendable later?
3. Did we discover that our convention/rule was wrong or incomplete?
4. Did we find a category of mistake (not a one-off bug) worth pre-empting?

**If yes to any → record.** If all no → skip silently (no noise in `docs/solutions/`).

## What to record

**Patterns**, not bug instances. The bug instance is in `git log`. The pattern is:

- The *category* of mistake (e.g., "forgetting to pull domain events from aggregate after mutation")
- The *trigger condition* (when does this category bite?)
- The *fix shape* (what to do; not what we did *this time*)
- Optional: cross-link to vault note that documents the underlying principle

## File shape

`docs/solutions/YYYY-MM-DD-<short-slug>.md`:

```markdown
---
date: 2026-MM-DD
session-context: <feature being worked on, or "general">
tags: [<categories>]
related-vault: ["wiki/concepts/<name>.md", …]   # if applicable
---

# <Short, descriptive title>

## The pattern

<2–3 sentences naming the category of mistake / discovery>

## When it bites

<concrete trigger conditions — when working on X, when doing Y, when refactoring Z>

## What to do

<the fix shape, in 2–4 bullets>

## Why this is true

<root cause / mechanism — why this category exists>

## Anti-pattern to avoid

<the wrong-but-tempting alternative>

## See also

- [vault: <path>]
- docs/adr/<NNNN-related.md> (if related)
```

## What to NOT record

- **Bug instances** (those are in `git log`).
- **One-off fixes** ("had to add a semicolon").
- **Things already documented** in `docs/adr/`, `docs/architecture.md`, or the vault.
- **Vague observations** ("be careful with async"). If you can't name the trigger, it's not actionable.

## After recording

Append a one-line entry to `docs/solutions/INDEX.md` for fast lookup:

```markdown
- 2026-MM-DD — <title> — <2-sentence summary>
```

## Periodic consolidation

The `/audit-docs` skill walks `docs/solutions/` quarterly and merges related entries into single distilled documents. Prevents log-style accumulation.
