# ADR-0012: Direct-call acquisition triggering (not event-driven)

- **Status:** Accepted
- **Date:** 2026-06-22
- **Deciders:** solo + Claude
- **Context tags:** [pattern | policy]

## Context

Saving a track and streaming a track both need to kick off audio acquisition:
on a fresh save (`AddTrackService`, dedup miss) and on a missing-file stream
(`StreamTrackService`, re-acquire). Both do this by calling a consumer-defined
port, `catalog/ports.AcquisitionScheduler`, whose production adapter is the
`acquisition` context's `BackgroundAcquisitionScheduler` (goroutine + bounded
semaphore + `inflight` dedup + `WaitGroup` drain on shutdown).

A catalog architecture review (deepening pass, 2026-06-22) moved the
save-path scheduling out of the HTTP handler and into `AddTrackService`. That
raised the obvious alternative: instead of catalog calling the scheduler, let
the `acquisition` context **subscribe** to the `track_added_to_library` domain
event that `AddTrackService` already publishes, fully decoupling the two
contexts. This decision records why we kept the direct port call.

The deciding fact is what the event bus actually is. `events.InProcessBus`
(`internal/shared/events/bus.go`) is a **per-user, in-process SSE notification
ring**: a fixed 100-event ring buffer per user, fan-out to subscriber channels
with a **non-blocking `default:` drop** when a channel is full
(`bus.go` ~line 75). It exists to push live updates to the mobile client over
SSE. It has no durability, no redelivery, no ack, no backpressure — by design,
because dropping a stale UI notification is harmless.

Acquisition is not harmless to drop. A dropped `track_added_to_library` event
means a saved track **silently never acquires audio** — it sits `pending`
forever with no signal. Wiring critical work onto a lossy notification bus would
trade a clean dependency diagram for a real reliability regression.

## Decision

Trigger acquisition by **direct call** through the `AcquisitionScheduler` port,
from both `AddTrackService` (save path, only on a fresh create) and
`StreamTrackService` (stream-miss re-acquire path). The scheduler is injected as
a nil-safe option (`WithAcquisitionScheduler`), nil when ffmpeg/storage are
absent. The scheduler's own `inflight` dedup and `WaitGroup` drain are the
durability story; the event bus is not in this path.

The `track_added_to_library` event is still published, but for **observation**
(logs, future analytics, SSE to the client) — never as the acquisition trigger.

## Alternatives considered

| Alternative | Why not |
|---|---|
| `acquisition` subscribes to `track_added_to_library` on the bus | The bus drops events on a full subscriber channel (`default:` in `Publish`). A drop = a saved track that silently never acquires. The bus is an SSE notification ring, not a work queue. |
| Make the in-process bus durable (ack + redelivery + backpressure) enough to carry acquisition | Real decoupling win, but it is a different component — an outbox/queue with delivery guarantees — not a tweak to the SSE ring. Deferred until there is a second consumer that justifies building it. |
| Keep scheduling in the HTTP handler (status quo before the review) | Splits the "add track" use case across the seam and puts domain policy in a thin adapter; the deepening review moved it into the service precisely to stop that. |

## Consequences

### What becomes easier
- "Add track" and "stream track" each own their acquisition trigger behind one
  small interface — one place to test, one place to change.
- No silent-no-acquire failure mode: the trigger path has no lossy hop.

### What becomes harder
- The `catalog` service layer depends (via a consumer-defined port) on a
  scheduler whose only production adapter lives in `acquisition`. That coupling
  is explicit and inward-pointing (port in catalog, adapter in acquisition), but
  it is a coupling an event bus would have hidden.

### What we're committing to (and the cost to reverse)
- If a durable in-process event transport is later built (outbox, queue), the
  event-driven path becomes viable and this ADR should be revisited — the
  `track_added_to_library` event is already published, so the trigger could move
  to a subscriber without touching `AddTrackService`'s core logic. Reversing is
  additive, not a rewrite.

## Implementation notes

- Save path: `AddTrackService.Execute` schedules only on `created == true`,
  forwarding the transient `AddTrackInput.SourceURL` (never persisted on the
  domain `Track`).
- Stream-miss path: `StreamTrackService.recoverMissingAudio` reschedules with an
  empty source URL (acquisition falls back to the search pipeline).
- Port: `catalog/ports.AcquisitionScheduler` (renamed from
  `ReacquisitionScheduler`, since the save path is initial acquisition).

## Vault references

- [vault: wiki/concepts/Hexagonal Architecture.md] — `AcquisitionScheduler` as a
  consumer-defined port; logic in the service, transport in the adapter.

## Related

- Architecture review (deepening pass) that surfaced the alternative: catalog
  candidates #1 (scheduling into `AddTrackService`) and #3 (reconcile inlined
  into `StreamTrackService`).
- `internal/shared/events/bus.go` — the lossy SSE ring this ADR declines to
  build acquisition on.
