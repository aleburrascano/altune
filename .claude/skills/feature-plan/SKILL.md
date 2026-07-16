---
name: feature-plan
description: |
  ALWAYS fires when the user says "plan this feature", "how should we build", "implementation strategy for",
  "break this down", "what's the approach for" — provided a spec for the named feature exists in
  docs/specs/<feat>/. Decomposes the spec into vertical slices (2–5 minutes of work each), each with file
  paths and a verification step. Dispatches plan-reviewer subagent before handoff to TDD.
when_to_use: |
  Use AFTER /feature-spec produces a reviewed spec. Use BEFORE /tdd-red-green-refactor. If user wants to
  plan without a spec, stop them and run /feature-spec first.
---

# Feature plan

## Mandatory first step

Read the spec at `docs/specs/<feat>/spec.md`. Then consult the pattern lexicon (`~/.claude/lexicon/`):
1. Scan the relevant manifest(s) for the patterns the spec mentions or implies.
2. `Read` the full entry for the top 2–3 hits at `~/.claude/lexicon/site/{path}/index.html`; lift each entry's *Cost:* line and anti-pattern guidance into the plan's "Risks" section.

## What this skill does

1. **Read the spec.** State the acceptance criteria back in plain language — confirm understanding before slicing.
2. **Vertical-slice decomposition.** Each slice is end-to-end demoable (touches domain + application + adapters where needed; or feature folder + tests on mobile). See `[vault: wiki/concepts/Vertical Slice Architecture.md]`.
3. **Size discipline.** Each slice = 2–5 minutes of implementation. If a slice is 20 lines and 4 files, that's the right size. If it's "implement feature X", split.
4. **Per-slice content** (in the plan file):
   - Goal (one sentence, matches one acceptance criterion)
   - Files touched (concrete paths)
   - Domain/application/adapter changes
   - Failing test to write first (file + test name)
   - Verification step (what command proves this slice is done)
5. **Order matters.** Slices ordered so each one is shippable on its own. No "slice 2 depends on slice 4 unfinished work".
6. **Write to** `docs/specs/<feat>/plan.md`.
7. **Dispatch plan-reviewer subagent.** Block on its output. Revise.
8. **Hand off** to user → `EnterPlanMode` confirmation → `/tdd-red-green-refactor` per slice.

## Plan file shape

```markdown
# <feat> — implementation plan

Spec: docs/specs/<feat>/spec.md

## Slices

### Slice 1: <one-sentence goal>
- Acceptance criterion: AC#1
- Files:
  - services/api/src/altune/domain/<context>/track.py (new)
  - services/api/tests/unit/altune/domain/<context>/test_track.py (new)
- Failing test first: `test_track_create_requires_title`
- Verify: `pytest tests/unit/altune/domain/<context>/test_track.py -v`

### Slice 2: …

## Risks
- (from vault anti-patterns + spec's "Risks" section)

## ADR candidates
- (if any decision is large enough to warrant an ADR, list here; the user confirms during review)
```

## Anti-patterns

- Coarse slices ("implement the API"). Push back; re-slice.
- Plans that bundle multiple unrelated changes. Split into multiple plans/specs.
- Slices that skip the "failing test first" line. TDD discipline starts at planning.
- Plans without explicit `verify` commands — those let "done" be subjective.
