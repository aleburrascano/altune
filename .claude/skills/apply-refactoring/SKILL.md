---
name: apply-refactoring
description: >
  Apply Fowler refactoring moves to a scope of code — find a smell, name the
  exact technique from the altune refactoring catalog, apply it, verify tests.
  An applier, not a review; pairs after /apply-design-patterns and before
  /improve-codebase-architecture.
disable-model-invocation: true
argument-hint: "[path|context|feature]   # default: current diff"
---

# Apply Refactoring

A find-and-fix pass over a single scope. The catalog of moves is `.claude/rules/refactoring/` — Fowler's techniques in six groups, adapted to altune's Go and RN/TS, each with smell · move · altune-form · when-to-skip. Read its [README.md](../../rules/refactoring/README.md) to pick the group, then the group file before applying.

## Scope
Default: the current working diff (`git diff` + staged). An argument narrows to a Go bounded context (`services/go-api/internal/<context>/`), a mobile feature (`apps/mobile/src/features/<feat>/`), or an explicit path.

## Process

1. **Read the scope in full.** Enumerate target files — don't sample.
2. **Name the move, not the smell.** Each finding cites the exact technique — "long service method → _Extract Method_ into a named helper," "primitive carrying domain meaning → _Replace Data Value with Object_." A finding that names no catalogued move is a vibe; drop it or sharpen it.
3. **Honor "When to skip it."** Every technique has a skip section — obey it. Don't extract a one-line helper; don't build an interface with one impl and one caller. The codebase is already tight, so expect few real findings and report honestly when a scope is clean.
4. **Apply surgically.** Match surrounding style. Touch only what the move requires — no adjacent "improvements." Prefer `mcp__serena__rename_symbol` / `replace_symbol_body` for symbol-level edits that ripple safely across call sites.
5. **Verify.** Refactoring is behavior-preserving, so no new failing test — but the suite MUST pass before and after (`go test ./...` / `npm test`). Tests are sacred: fix the implementation to keep them green, never the test.

## Output
One line per fix — `<technique> on <file> — <why>` — plus smells found-but-skipped with the reason. Then the diff.

## Chain
**/apply-design-patterns** (target shapes) → **/apply-refactoring** (the moves that get there) → **/improve-codebase-architecture** (deep-module check). One narrow pass each; this skill is the middle link.
