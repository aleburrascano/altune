# shared/telemetry — router

Rotating `session_id` correlation, the two-tier reliability outbox, and the unified `useRecordEvent` hook.

Invariants:

- `library_add` and `wrong_album` are label-critical: they go through `enqueueCritical` (client-minted `event_id`, server dedup on conflict), never fire-and-forget.
- Everything else uses `useRecordEvent` — best-effort, errors swallowed to `console.warn` per ADR-0007 §3.12; telemetry is never surfaced to the user.
- Keep new logic in the pure half of the pure-function/stateful-wrapper split (`advanceSession`, `outbox.ts`); the stateful half is tested too — `flushOutbox`'s reentrancy guard, its commit-after-send ordering, and both `outboxStore` branches all carry tests.
- The outbox IS durable across a hard app-kill: it mirrors to a JSON file via `outboxStore.ts` (`expo-file-system`, already a dependency — no new native module, no ADR). Disk failures degrade to in-memory-only, never throw.
- `flushOutbox` drops an entry the server rejects with 400 and keeps draining; every other failure stops the drain and retries later.
- Never send a payload key Go constrains to a string as `null` — omit the key instead.
- Guard every `Directory.create` with `if (!dir.exists)`; the native call throws on an existing directory unless `idempotent` is set.

Tests: `__tests__/` — `outbox`, `outbox.pure`, `outbox.property`, `outbox.restore`, `outboxStore`, `session`, `recordEvent`, `useRecordEvent`, `eventContract`, `slice-invariants`. Categories and rejections: `okf/testing/shared-telemetry.md`.

Knowledge base: `okf/mobile/shared-telemetry.md`; backend consumer: `okf/backend/discovery/telemetry.md` — read before structural work; update in the same commit when behavior it describes changes (pre-commit hook enforces).
