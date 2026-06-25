# State — Behavioral

> GoF behavioral pattern. Source: https://refactoring.guru/design-patterns/state

**Intent.** Let an object alter its behavior when its internal state changes — as if it changed its class.

## Problem
An object's behavior depends on a mode that grows over time, scattering `switch (state)` conditionals across many methods. Adding a state means touching every conditional; transition rules get duplicated and drift.

## Solution
Model each state explicitly and route behavior through the current state. Transitions replace the active state. Unlike Strategy, states may know about and trigger transitions to one another.

## In altune
**Go:** No state classes — model the state as a **typed enum** and `switch` on it, with behavior on the typed value. **Verified:** `AcquisitionStatus int` (`AcquisitionPending`/`AcquisitionReady`/`AcquisitionFailed`) with a `String()` method and `ParseAcquisitionStatus`, at `services/go-api/internal/catalog/domain/track.go:37`. Transition rules and the `audio_ref ↔ status` invariant live on the `Track` aggregate, enforced at method boundaries — that's the State machine, made illegal-states-unrepresentable rather than a strategy-object swap. Enums start with a zero-value sentinel so an uninitialized state can't masquerade as a real one.
**RN/TS:** A **discriminated union** is the State pattern. `RepeatMode = 'off' | 'all' | 'one'` at `apps/mobile/src/shared/playback/types.ts:34`; the `Queue` (`apps/mobile/src/shared/playback/queueStore.ts`) is a small state machine — `currentIndex`, `shuffled`, `repeatMode` drive `skipToNext`/`cycleRepeatMode` transitions. Async UI uses the `loading | loaded | error` union (never nullable fields) so each state's render is exhaustive.
<Verified: `services/go-api/internal/catalog/domain/track.go:37` (AcquisitionStatus); `apps/mobile/src/shared/playback/types.ts:34` (RepeatMode).>

## When to reach for it
- Behavior genuinely varies by a runtime mode with multiple states and transitions.
- A `switch (state)` recurs across 3+ methods (Rule of Three) — promote to a typed state with behavior attached.

## When to skip it
Two states and one `if` — a boolean is cheaper than a state machine. Don't manufacture per-state objects when an enum + a single `switch` reads clearly.

## When State vs Strategy
State's variants trigger their own transitions and depend on each other; [[strategy]]'s algorithms are independent and chosen by the client. Same structural shape, different intent.

## Related
- Patterns: [[strategy]] (sibling — independent algorithms vs. self-transitioning modes), [[command]], [[memento]] (snapshot a state machine)
- Refactoring moves: `../../refactoring/organizing-data.md` (Replace Type Code with State/Strategy; Replace Type Code with Class), `../../refactoring/simplifying-conditional-expressions.md` (Replace Conditional with Polymorphism)
- Project rules: `../../backend/go-design-patterns.md`, `../../backend/domain-layer.md`, `../../frontend/rn-state-management.md`
