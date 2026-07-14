# Observer — Behavioral

> GoF behavioral pattern. Source: https://refactoring.guru/design-patterns/observer

**Intent.** Define a subscription mechanism so multiple objects are notified of events in the object they observe.

## Problem
Some objects need to react when another's state changes, but polling wastes work and the publisher shouldn't be coupled to a fixed, compile-time-known set of dependents. The set of interested parties may change at runtime.

## Solution
The publisher keeps a list of subscribers and notifies them through a uniform interface when an event occurs. Subscribers attach/detach dynamically; the publisher never knows their concrete types.

## In altune
**Go:** **Domain events** are the observer surface — `SearchPerformed`, `ResultClicked` (discovery), `TrackAddedToLibrary` (catalog): past-tense, immutable structs with `OccurredAt`, raised by aggregate methods and consumed by the application layer (emitted to logs in v1; future specs may add analytics consumers). Aggregates don't hold subscriber lists (that would couple domain to consumers — see `domain-layer.md`); the service layer dispatches. For in-process fan-out, a channel or a slice of handler funcs is the idiomatic mechanism — not a `Subject`/`Observer` interface pair.
**RN/TS:** The observer mechanism is **React render + the TanStack Query cache**, not hand-rolled listeners. Components subscribe by reading a query/store selector; a cache update or store `set` re-renders subscribers automatically. Zustand selectors are the subscribe-with-fine-grained-notification primitive. Don't build an `EventEmitter` for UI state.
<Conceptual — domain events verified in ubiquitous-language; React/Query/Zustand are the RN observer substrate.>

## When to reach for it
- One state change must notify an open/dynamic set of consumers (domain events).
- Decoupled fan-out where the publisher must not know its subscribers.

## When to skip it
All dependents are known and fixed at compile time — a direct call is clearer. On RN, never hand-roll listeners when a store selector or query subscription already gives you reactive notification. Observers give no ordering guarantee — if you need ordered handling, this isn't it.

## Related
- Patterns: [[mediator]] (a mediator can be the publisher), [[command]], [[chain-of-responsibility]]
- Refactoring moves: `../../refactoring/organizing-data.md` (Duplicate Observed Data — the hexagonal/render boundary makes the one-way sync that Observer formalizes)
- Project rules: `../../../.claude/rules/backend/domain-layer.md` (domain events), `../../../.claude/rules/frontend/rn-state-management.md`
