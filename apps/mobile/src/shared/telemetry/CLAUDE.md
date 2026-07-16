# shared/telemetry — router

Rotating `session_id` correlation, the two-tier reliability outbox, and the unified `useRecordEvent` hook.

Invariants:

- `library_add` and `wrong_album` are label-critical: they go through `enqueueCritical` (client-minted `event_id`, server dedup on conflict), never fire-and-forget.
- Everything else uses `useRecordEvent` — best-effort, errors swallowed to `console.warn` per ADR-0007 §3.12; telemetry is never surfaced to the user.
- The pure-function/stateful-wrapper split (`advanceSession`, `outbox.ts`) is deliberate: pure logic is unit-tested, the trivial stateful shell is not — keep new logic in the pure half.
- The outbox does NOT survive a hard app-kill while offline (documented; AsyncStorage is ADR-gated future work) — don't rely on it for durable writes.

Knowledge base: `okf/mobile/shared-telemetry.md`; backend consumer: `okf/backend/discovery/telemetry.md` — read before structural work; update in the same commit when behavior it describes changes (pre-commit hook enforces).
