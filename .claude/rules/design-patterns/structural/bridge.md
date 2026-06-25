# Bridge — Structural

> GoF structural pattern. Source: https://refactoring.guru/design-patterns/bridge

**Intent.** Split a thing that varies along two independent dimensions into two hierarchies — abstraction and implementation — so each can change without multiplying the other.

## Problem
When a type varies on two axes at once (shape × color, renderer × platform), inheritance forces one subclass per combination — `RedCircle`, `BlueSquare` — and each new value on either axis multiplies the class count. Combinatorial explosion.

## Solution
Extract one axis into its own interface and hold it as a field (composition), rather than subclassing for it. The "abstraction" delegates the varying work to an injected "implementation" object, so the two axes scale additively, not multiplicatively, and the implementation can be swapped at runtime.

## In altune
**Go:** Rare, and usually it's just ordinary dependency injection — there's no inheritance to escape, so the "explosion" Bridge solves doesn't arise the same way. The idiomatic equivalent: a struct that takes a small interface as a field and delegates the varying axis to it (a use case holding a `ports.SearchProvider`, swappable per provider). Don't invoke "Bridge" by name for this; it's DI via interface (`../../backend/go-dependency-injection.md`). Reach for the explicit Bridge framing only if you genuinely have two orthogonal axes both growing.
**RN/TS:** Conceptual. A component parameterized over a strategy/renderer passed as a prop or hook (presentation axis × data-source axis) approximates it. Function components compose; they don't inherit, so the explosion never starts.

No verified instance — Bridge does not appear as a deliberate pattern in the codebase. Mapped as "DI via interface" where the two-axis case would otherwise arise.

## When to reach for it
- A type genuinely varies on two independent axes that both keep gaining values.
- You need to swap the implementation axis at runtime.

## When to skip it
- One axis varies, or one axis is fixed — plain interface injection (or no abstraction at all) is clearer. Bridge for a single dimension is ceremony.
- You only have one implementation of the second axis — discover the interface later (`../../backend/go-structs-interfaces.md`: "don't design with interfaces, discover them"), don't pre-split.

## Related
- Patterns: [[adapter]] (retrofits an *existing* incompatible interface; Bridge is designed in upfront), [[strategy]] (Bridge shares Strategy's composition shape but partitions a whole class, not just an algorithm)
- Refactoring moves: `../../refactoring/dealing-with-generalization.md` — Extract Interface; `../../refactoring/organizing-data.md` — Replace Type Code with State/Strategy
- Project rules: `../../backend/go-dependency-injection.md`, `../../backend/go-structs-interfaces.md`
