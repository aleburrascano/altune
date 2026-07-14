# Mediator — Behavioral

> GoF behavioral pattern. Source: https://refactoring.guru/design-patterns/mediator

**Intent.** Reduce chaotic many-to-many dependencies by routing component collaboration through one mediator object.

## Problem
Components that talk directly to many peers form a tangle: changing one ripples through the others, and none can be reused in isolation. The coupling grows quadratically with the number of collaborators.

## Solution
Components depend only on a mediator interface, not on each other. The mediator receives a component's notification and decides which others to invoke. New collaboration patterns are added in the mediator, leaving components untouched.

## In altune
**Go:** Largely **already the service layer**. A use case in `service/` is the mediator between inbound handlers and the ports it coordinates (providers, repos, caches) — handlers and adapters don't know each other, only the service wiring. Don't introduce a *separate* mediator object on top of this; the hexagonal application layer is the mediator, and adding another indirection layer would be a pass-through god-object (the "Remove Middle Man" smell).
**RN/TS:** A **Zustand store** mediates between components that would otherwise prop-drill or cross-reference: components read/dispatch through the store, never directly into each other. The playback store coordinates now-playing UI, queue UI, and controls without those components knowing one another.
<Conceptual — service layer and Zustand stores are the real mediators; no dedicated mediator class.>

## When to reach for it
- A genuine N×N coupling between peers that a central coordinator can flatten.
- Cross-component coordination that doesn't belong in any one component (a store).

## When to skip it
You already have a service layer or store doing the mediation — don't add a second one. A few naturally-independent components with one or two interactions don't need a mediator; it would just centralize complexity for its own sake.

## Related
- Patterns: [[observer]] (a mediator often notifies via subscriptions), [[command]], [[chain-of-responsibility]]
- Refactoring moves: `../../refactoring/moving-features-between-objects.md` (Hide Delegate; Remove Middle Man — know when the mediator earns its place vs. is dead-weight forwarding)
- Project rules: `../../../.claude/rules/backend/go-design-patterns.md`, `../../../.claude/rules/frontend/rn-state-management.md`
