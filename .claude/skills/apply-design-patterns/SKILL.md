---
name: apply-design-patterns
description: >
  Apply GoF design-pattern shapes to a scope of code — recognize where a
  Strategy / Facade / State / etc. form would deepen a module, name it from the
  altune pattern catalog, and apply it in idiomatic Go or RN/TS. An applier, not
  a review; pairs after /apply-refactoring and before /improve-codebase-architecture.
disable-model-invocation: true
argument-hint: "[path|context|feature]   # default: current diff"
---

# Apply Design Patterns

A recognize-and-apply pass over a single scope. The catalog of target shapes is `.claude/rules/design-patterns/` — the 23 GoF patterns remapped to altune's Go-hexagonal and RN/TS-functional stacks, each with a verdict (✅ live in repo · ◐ conceptual · ⊘ N/A) and a "when to skip it". Read its [README.md](../../rules/design-patterns/README.md) for the map, then the specific pattern file before applying.

## Scope
Default: the current working diff (`git diff` + staged). An argument narrows to a Go bounded context (`services/go-api/internal/<context>/`), a mobile feature (`apps/mobile/src/features/<feat>/`), or an explicit path.

## Process

1. **Read the scope in full.** Enumerate the changed/target files — don't sample.
2. **Recognize, don't impose.** Where structure is ad-hoc — a growing `switch (kind)`, a fat constructor, a hand-rolled cache, branchy per-provider logic — name the GoF shape it wants and cite the catalog file (`design-patterns/behavioral/strategy.md`). A spot that names no catalogued shape is left alone.
3. **Honor "When to skip it."** Every pattern file has a YAGNI/KISS section — obey it. One implementation, one caller → no pattern. Rule of Three before a Strategy table. The backend and mobile app are already tight, so most scopes yield few or zero real applications — say so plainly rather than inventing work.
4. **Apply in altune form.** Go: interfaces-where-consumed, struct embedding, function values, functional options — never classes. RN/TS: hooks, component composition, discriminated unions, strategy tables — never class components. Match surrounding style; touch only what the shape requires.
5. **Verify.** The suite passes before and after (`go test ./...` / `npm test` in the scope). Tests are sacred — never edit them to fit the change. This is shape, not behavior: nothing observable should move.

## Output
One line per change — `<pattern> applied to <file> — <why>` — plus anything recognized-but-skipped with its skip reason. Then the diff.

## Chain
Run after landing a batch of features: **/apply-design-patterns** (shapes) → **/apply-refactoring** (the moves that get there) → **/improve-codebase-architecture** (deep-module locality). Each is one narrow pass; this skill is just the first link.
