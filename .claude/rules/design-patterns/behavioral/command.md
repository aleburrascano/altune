# Command — Behavioral

> GoF behavioral pattern. Source: https://refactoring.guru/design-patterns/command

**Intent.** Turn a request into a stand-alone object carrying everything needed to perform it later.

## Problem
Triggers (buttons, menu items, queue consumers) get coupled to concrete business logic, duplicated wherever the same operation is invoked. You also can't easily queue, log, schedule, or undo an operation that's just an inline method call.

## Solution
Wrap the request — receiver, method, args — in a command object with a uniform `execute()`. Invokers hold commands, not receivers, so the same operation triggers from many places, queues, or reverses (with a paired `undo`).

## In altune
**Go:** No command *classes*; the idiomatic equivalent is a **closure** (`func() error`) or a small struct capturing inputs + a method. A use case in `service/` already *is* a parameterized operation object (struct with injected ports + an `Execute(ctx, input)` method) — that's Command at the application-layer grain. Deferred/queued work (a job pushed to a worker) is a serialized command. Undo is not modeled today.
**RN/TS:** A TanStack Query **mutation** is Command-shaped: `mutationFn` + variables packaged as a unit, invoked from any component, with `onMutate`/`onError` giving optimistic-apply + rollback (a lightweight undo). Encode user intents as discriminated-union action objects (`{ type: 'save'; trackId }`) dispatched to a store handler rather than calling store internals directly.
<Conceptual — service `Execute` and TanStack mutations are the real shapes; no GoF command object.>

## When to reach for it
- You need to queue, schedule, log, or replay operations.
- Undo/redo (capture inverse state — pairs with [[memento]]).
- One operation invoked from multiple UI entry points.

## When to skip it
A direct method call that's invoked once, never queued, never undone — wrapping it in a command object is pure ceremony (KISS).

## Related
- Patterns: [[memento]] (undo via saved state), [[strategy]] (same struct shape, different intent — swap algorithm vs. parameterize an action), [[chain-of-responsibility]]
- Refactoring moves: `../../refactoring/simplifying-method-calls.md` (Replace Parameter with Explicit Methods; Introduce Parameter Object — a command *is* the parameter object for an operation)
- Project rules: `../../backend/go-design-patterns.md`, `../../frontend/rn-state-management.md`
