---
type: Subsystem
title: Discovery telemetry & behavioral signals
description: The InteractionEvent pipeline that persists every search/engagement event correlated by search_id, and the SatisfactionConsumer that turns play/skip/completed into a ranking signal.
resource: services/go-api/internal/discovery/domain/events.go, services/go-api/internal/discovery/service/behavioral_signals.go, services/go-api/internal/discovery/service/record_event.go, services/go-api/internal/discovery/service/telemetry.go, services/go-api/internal/discovery/adapters/persistence/event_repo.go, services/go-api/internal/discovery/ports/ports_telemetry.go
tags: [discovery, telemetry, events, behavioral-ranking, persistence, subsystem]
verified_commit: 0352abc38e1235b81f063759748d2c01dfa9b9af
---

`domain.InteractionEvent` (`domain/events.go`) is the single append-only envelope for every behavioral event: `EventType` (search_performed, results_shown, result_clicked, play, skip, completed, library_add, wrong_album — closed vocabulary, zero-value `EventTypeUnknown` sentinel), `SearchId` (the keystone join key back to the originating search), `EventId` (client-minted idempotency key for the label-critical outbox tier), `ClientOccurredAt` vs. server `OccurredAt`, and a JSONB `Payload` for schema-free growth. Persisted in `discovery_events` (see [discovery-events-table](../../data/discovery-events-table.md)).

Two write paths feed it. `emitSearchEvent` (`service/telemetry.go`) fires-and-forgets a `search_performed` event asynchronously off the request path (`context.WithoutCancel` + its own timeout + panic recovery), stamping `tail_noise_top5`, the exploration flag, and the top-10 shown slate. `RecordEventService` (`service/record_event.go`) is the inbound use case for client-emitted events (play/skip/library_add/wrong_album), validating `EventType != Unknown` before calling `ports.EventStore.Append`.

`PgxEventStore` (`adapters/persistence/event_repo.go`) is the sole adapter satisfying five read/write ports (`EventStore`, `EventQuery`, `BehavioralSignalStore`, `BehavioralLabelStore`, `SessionSignalStore`) over one `discovery_events` table. Its `SatisfactionSignals` query nets +1 per play/completed and −1 per skip with dwell under the 20s short-dwell threshold (Kim WSDM 2014), grouped by `result_signature`. `AbandonedSearches` joins on a `session_id` carried in the JSONB payload to find no-click searches reformulated within 60s.

`ports.EventConsumer` (`ports/ports_telemetry.go`) is the Strategy seam: `SatisfactionConsumer` (`service/behavioral_signals.go`) is the first implementation. `Service.RefreshBehavioralScores` recomputes the score map off the request path (a background ticker via `StartBehavioralRefresh`) and atomically swaps it into an `atomic.Pointer`; the search path only ever reads the published snapshot (`Service.BehavioralScoresSnapshot`, exported so the Mission Control re-run ranks with the same behavioral input — structure-audit F1, 2026-07-16), gated behind `behavioralRanking` — a new signal (pogo-sticking, corpus labels) is a new `EventConsumer`, never a pipeline rewrite. Feeds the offline eval loop (see [eval-harness](eval-harness.md)).
