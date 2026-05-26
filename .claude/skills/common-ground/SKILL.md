---
name: common-ground
description: |
  Fires from SessionStart (lite mode) and explicitly when user says "what are you assuming",
  "surface assumptions", "common ground", "what do you think we're building", "play back the spec".
  Surfaces Claude's hidden assumptions about the project, current feature, and immediate task in
  3–5 bullets so the user can correct before work proceeds.
when_to_use: |
  Use at session start (lite — 3 bullets only). Use when starting a new feature or after a long
  break. Use when the user senses misalignment.
---

# Common ground

## What this skill does

Forces Claude to state explicitly what it currently believes about:

1. **What this project is** (1 line, from CLAUDE.md + recent commits)
2. **What feature/task is active** (from recent docs/specs/ activity + git status + open files in context)
3. **What approach Claude is currently leaning toward** (when there's an in-flight decision)
4. **What Claude is unsure about** (open questions that would materially change the work)
5. **What Claude assumes** (the implicit choices it's about to make silently)

## Format

```
## Common ground (<lite | full>)

**Project:** Altune — music manager, Expo + Python, hexagonal backend, vertical-slice mobile.

**Active context:** working on `<feature-name>`; spec at docs/specs/<feat>/spec.md exists at <state>; plan <exists / does not exist>.

**Current leaning:** I plan to <approach summary>. This relies on assumptions:
- <assumption 1>
- <assumption 2>

**Unsure about:**
- <open question 1>
- <open question 2>

**Please correct anything wrong before I proceed.**
```

## Lite mode (SessionStart)

Just project + active context + the 1–2 biggest open assumptions. ≤6 lines total.

## Full mode (explicit invocation)

All 5 sections, no length limit but stay scannable.

## When the user says "you got it right"

Move on. Don't re-confirm.

## When the user corrects an assumption

Update your understanding immediately. If the correction reveals a missing rule, suggest adding it to the right place (`CLAUDE.md` if global, `.claude/rules/` if path-scoped, nested CLAUDE.md if feature-local).

## Anti-patterns

- Stating only what Claude knows is right (the point is to expose what might be wrong).
- Vague assumptions ("I'm assuming you want good code") — be specific or skip.
- Skipping when there's clearly misalignment ("I'll just start and we'll see") — that's how rework happens.
