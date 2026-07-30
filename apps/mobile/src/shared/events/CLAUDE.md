# shared/events — router

The mobile half of the SSE event bus: `sse-client.ts` (hand-rolled EventSource over XHR, with watchdog/recycle/backoff), `useServerEvents.ts` (lifecycle + AppState), `applyServerEvent.ts` (pure router from event to cache/store effects), and the cache patchers `trackCachePatch.ts` / `playlistCachePatch.ts`. Event names are declared in `eventTypes.ts`.

Invariants:

- Every event type the Go API publishes is declared in `eventTypes.ts` and handled in `applyServerEvent.ts`; the contract test derives the published set from `services/go-api` source, so a backend deploy that adds an event fails this suite rather than silently dropping it.
- Prefer patching the query cache over invalidating it; `resync` is the only full-reconcile path.
- Patch every cache family that holds a Track — library pages, lookup, featuring, and playlist details — or one screen goes stale while another updates.
- Keep `track_count` in agreement between `playlistKeys.detail(id)` and `playlistKeys.list`; recompute from the array rather than decrementing a stored number.
- A handler must not write a field the event did not carry — omit the key instead of writing `null`, or a thin redelivery erases good data.
- Every handler is idempotent: applying the same event twice equals applying it once, because the server can redeliver.
- Parse the wire format per the SSE spec, not per what the current server happens to send: `\r\n` terminators, multiple `data:` lines joined with `\n`, and the space after the colon optional.

Tests: `__tests__/` — `applyServerEvent`, `trackCachePatch`, `playlistCachePatch`, `sse-client`, `useServerEvents`, `eventTypes`, `eventContract`. Categories and rejections: `okf/testing/shared-events.md`.

Knowledge base: `okf/mobile/shared-events.md`; backend counterpart: `okf/backend/app-wiring.md` — read before structural work; update in the same commit when behavior it describes changes (pre-commit hook enforces).
