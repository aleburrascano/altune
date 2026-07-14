# Iterator — Behavioral

> GoF behavioral pattern. Source: https://refactoring.guru/design-patterns/iterator

**Intent.** Traverse the elements of a collection without exposing its underlying representation.

## Problem
Collections differ internally (slice, tree, paged API), yet clients want a uniform way to walk them. Baking traversal into the collection bloats it and couples callers to its structure; you also want lazy/streaming traversal that doesn't materialize everything.

## Solution
Extract traversal into an iterator that holds the cursor state and yields one element at a time behind a uniform contract. Clients consume elements without knowing the backing structure, and large/infinite sequences stream lazily.

## In altune
**Go:** Go 1.23+ **range-over-func iterators** (`iter.Seq[T]` / `iter.Seq2[K,V]`) are the native, endorsed form — `go-design-patterns.md` recommends them for lazy evaluation and for streaming large transfers (DB→HTTP) without loading everything into memory (avoids OOM). A function returning `func(yield func(T) bool)` is consumed with `for v := range seq`. Prefer this over hand-rolled `Next()`/`HasNext()` cursor structs.
**RN/TS:** Native generators (`function*` / `Symbol.iterator`) exist but are rarely needed — paginated server data is owned by TanStack Query's `useInfiniteQuery` (pages + `fetchNextPage`), which *is* the iteration seam. In-memory arrays use `.map`/`for...of` directly; don't build an iterator object.
<Conceptual — verify a specific `iter.Seq` usage in discovery before citing a file; the pattern is endorsed, instances vary.>

## When to reach for it
- Lazy/streaming traversal of a large or generated sequence (Go `iter.Seq`).
- Hiding a non-trivial backing structure (tree, graph, paged source) behind uniform iteration.

## When to skip it
A plain slice or small array — `range`/`for...of` already is the iterator. Wrapping it adds indirection with no payoff.

## Related
- Patterns: [[visitor]] (operate while traversing), [[memento]] (capture iteration position)
- Refactoring moves: `../../refactoring/composing-methods.md` (Extract Method — pull traversal out of the collection)
- Project rules: `../../../.claude/rules/backend/go-design-patterns.md`, `../../../.claude/rules/backend/go-data-structures.md`
