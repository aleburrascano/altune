# Composite — Structural

> GoF structural pattern. Source: https://refactoring.guru/design-patterns/composite

**Intent.** Compose objects into tree structures and let client code treat individual leaves and whole containers through one uniform interface.

## Problem
Some domains are naturally recursive — boxes inside boxes, a UI node with child nodes, a query AND-ing sub-queries. Processing such a tree by branching on "is this a leaf or a container?" at every level couples the client to concrete types and nesting depth, and the logic gets unwieldy.

## Solution
Define one interface satisfied by both leaves and containers. A leaf returns its own value; a container recurses into its children and aggregates their results. The client calls the same method on any node and lets the recursion handle the shape.

## In altune
**Go:** Conceptual, used sparingly. The shape is a recursive interface — `type Node interface { Resolve(ctx) Result }` — with a leaf struct and a container struct holding `[]Node`, both satisfying it; the container's method ranges its children and folds. Idiomatic when a real tree exists (a nested filter/predicate tree, a composable scoring expression). The discovery pipeline today is a flat scatter-gather, not a tree, so no live instance.
**RN/TS:** This is React's native model — components render children, and a parent renders the same way whether a child is a leaf or another composite. `children` composition *is* Composite. You rarely name it; you just nest components.

No verified Go instance — would materialize only if a recursive domain tree (predicate/expression tree) appears. RN composition is structural-by-default.

## When to reach for it
- The domain is a genuine part-whole tree and clients should not care about depth.
- You want one operation (render, evaluate, total) to run uniformly over leaves and branches.

## When to skip it
- The data is a flat list, not a tree — a slice + `range` beats a node interface (Go: `make([]T, 0, n)`, not a fake hierarchy).
- The "tree" is two levels deep and fixed — a parent struct with a typed slice field is simpler than a recursive interface (KISS). Add the interface when a third level or a second leaf type forces it (Rule of Three).

## Related
- Patterns: [[decorator]] (same recursive-wrapping structure, but Decorator wraps *one* child to add behavior; Composite aggregates *many* children), [[flyweight]] (share immutable leaf nodes to cut tree memory)
- Refactoring moves: `../../refactoring/organizing-data.md` — Replace Array with Object; `../../refactoring/simplifying-conditional-expressions.md` — Replace Conditional with Polymorphism (the leaf/container dispatch)
- Project rules: `../../../.claude/rules/backend/go-structs-interfaces.md`, `../../../.claude/rules/backend/go-data-structures.md`
