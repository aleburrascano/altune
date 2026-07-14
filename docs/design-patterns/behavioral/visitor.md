# Visitor — Behavioral

> GoF behavioral pattern. Source: https://refactoring.guru/design-patterns/visitor

**Intent.** Separate an algorithm from the object structure it operates on, so new operations can be added without changing the elements.

## Problem
You need to add behaviors (export, analyze, transform) across a hierarchy of element types without polluting each element with unrelated methods, and without editing stable element classes every time a new operation appears.

## Solution
Move each operation into a visitor object with one method per element type; elements `accept(visitor)` and call back the matching method — **double dispatch**: the runtime type of the element *and* of the visitor together select the code. New operations are new visitors; elements stay closed.

## In altune
**Go:** Essentially **N/A**. Go has no double-dispatch idiom — no method overloading by argument type, no class hierarchy to `accept` into. The native alternatives:
- A **type switch** over a small interface — `switch e := elem.(type) { case Artist: …; case Album: … }` — covers "different behavior per element type" directly, at the cost of editing the switch when a type is added.
- A per-kind **strategy table** (`map[ResultKind]func(...)`) when the same dispatch recurs.
Reach for these, not a hand-built visitor with `accept`/`visit` pairs — that machinery buys nothing without language-level double dispatch.
**RN/TS:** **N/A** in the same way. A discriminated union + exhaustive `switch (node.kind)` (with a `never` default for compile-time totality) is the idiomatic substitute; a `Record<Kind, (node) => …>` table for recurring dispatch.
<N/A — no double-dispatch idiom in Go or RN-functional; use type switch / discriminated-union switch / strategy table.>

## When to reach for it
- Essentially never in this stack. If you find yourself wanting Visitor, you want a typed switch or a strategy lookup.

## When to skip it
Always, here. The element set is small and the operations few → a `switch` is clearer. The only thing Visitor adds over a switch is open/closed-on-operations via double dispatch, which neither Go nor RN-functional provides cleanly.

## Related
- Patterns: [[strategy]] (the real answer — per-type behavior via interface/table), [[iterator]] (traverse then operate), [[command]]
- Refactoring moves: `../../refactoring/simplifying-conditional-expressions.md` (Replace Conditional with Polymorphism — the compositional dispatch that replaces a visitor), `../../refactoring/organizing-data.md` (Replace Type Code with State/Strategy)
- Project rules: `../../../.claude/rules/backend/go-structs-interfaces.md` (type switches), `../../../.claude/rules/frontend/rn-state-management.md`
