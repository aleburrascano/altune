---
name: apply-design-patterns
description: >
  Apply GoF design-pattern shapes to a scope of code — read every file, hold the
  whole pattern catalog in view, and apply whichever shapes genuinely deepen a
  module, in idiomatic Go or RN/TS. An applier, not a review.
disable-model-invocation: true
argument-hint: "[path|context|feature]   # default: current diff"
---

# Apply Design Patterns

A recognize-and-apply pass over a single scope. The catalog of target shapes is `.claude/rules/design-patterns/` — the 23 GoF patterns remapped to altune's Go-hexagonal and RN/TS-functional stacks, each with a verdict (✅ live in repo · ◐ conceptual · ⊘ N/A) and a "when to skip it". Read its [README.md](../../rules/design-patterns/README.md) for the full map and hold all 23 in view — this skill is about leveraging as many as honestly fit, not hunting one or two.

## Scope
Default: the current working diff (`git diff` + staged). An argument narrows to a Go bounded context (`services/go-api/internal/<context>/`), a mobile feature (`apps/mobile/src/features/<feat>/`), or an explicit path.

## Process

1. **Read every file in the scope, in full.** Enumerate them, report the count, read all of them — if there are 100, read 100. Don't sample, and don't pre-filter to files that "look patternish". The whole catalog is the lens; you can't see which shape a file wants until you've read it.
2. **Consider the whole catalog against each file — don't search by smell.** For each file, ask "does any of the 23 shapes deepen this?", not "where is the `switch` / the fat constructor". Searching for one named pattern at a time (or fanning out one sub-agent per smell) blinds you to the other 22 — the bias this skill exists to avoid. When a shape fits, name it and cite its catalog file (e.g. `design-patterns/behavioral/strategy.md`); a file that wants no catalogued shape is left alone.
3. **Honor "When to skip it."** Every pattern file has a YAGNI/KISS section — obey it. One implementation, one caller → no pattern; Rule of Three before extracting a seam. The backend and mobile app are already tight, so a scope yielding few or zero real applications is the expected, honest outcome — say so rather than inventing work.
4. **Apply in altune form.** Go: interfaces-where-consumed, struct embedding, function values, functional options — never classes. RN/TS: hooks, component composition, discriminated unions, strategy tables — never class components. Match surrounding style; touch only what the shape requires.
5. **Verify.** The suite passes before and after (`go test ./...` / `npm test` in the scope). Tests are sacred — never edit them to fit the change. This is shape, not behavior: nothing observable should move.

## Output
One line per change — `<pattern> applied to <file> — <why>` — plus anything considered-but-skipped with its skip reason. Then the diff.
