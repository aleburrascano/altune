# Memento — Behavioral

> GoF behavioral pattern. Source: https://refactoring.guru/design-patterns/memento

**Intent.** Capture and externalize an object's state so it can be restored later, without violating its encapsulation.

## Problem
Undo, rollback, or resume-on-reopen needs a snapshot of internal state — but exposing all fields to a snapshot-taker breaks encapsulation and makes the type fragile to change.

## Solution
The originator produces an opaque snapshot (memento) of its own state; a caretaker stores it without inspecting it and hands it back to restore. State capture and restore stay inside the originator.

## In altune
**Go:** Classic class-based Memento is **N/A** (no private-field-exposure problem to solve via a friend class). The native shape: a use case serializes an aggregate's restorable state into a plain value (or persists a snapshot row) and rehydrates from it. Value objects are immutable, so a "snapshot" is often just keeping the prior value.
**RN/TS:** **Verified instance — playback resume-on-reopen.** The `Queue` (tracks, `playOrder`, `currentIndex`, `repeatMode`, `shuffled`, `source`) plus saved playback position is snapshotted and persisted (server-side queue snapshot; client restores the saved position on relaunch — the recent "resume playback at saved position on relaunch" work). The store is the originator; the persistence layer is the caretaker; the serialized snapshot is the memento. See `apps/mobile/src/shared/playback/queueStore.ts` (`QueueState` is the snapshot-able shape; `loadQueue` rehydrates it).
<Verified: `apps/mobile/src/shared/playback/queueStore.ts` — `QueueState` interface + `loadQueue` restore path.>

## When to reach for it
- Resume-on-reopen / session restore (the playback case).
- Undo/redo or transactional rollback where you snapshot before mutating.

## When to skip it
The object is small and immutable — keep the old value, no memento machinery. If you don't need to restore, don't snapshot. Frequent large snapshots with tight memory budgets argue against it.

## Related
- Patterns: [[command]] (undo pairs a command with a memento), [[iterator]] (snapshot traversal position)
- Refactoring moves: `../../refactoring/organizing-data.md` (Encapsulate Field / Encapsulate Collection — the encapsulation a memento must respect)
- Project rules: `../../../.claude/rules/frontend/rn-state-management.md` (Zustand `persist` middleware is the caretaker mechanism)
