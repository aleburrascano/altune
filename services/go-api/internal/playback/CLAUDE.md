# Playback (server) context — router

Deliberately thin (ADR-0010): the live Queue is client-owned (`apps/mobile/src/shared/playback/`); this context persists only a snapshot.

Layout:

- `domain/queue_state.go` — `QueueState`, `QueueStateInput`, the `newQueueState` gate, `RepeatMode`, `ValidationError`.
- `domain/queue_source.go` — `QueueSource` and the `Format`/`ParseQueueSource` pair owning the stored `source_id` packing.
- `ports/` — `QueueStateRepository`, `NowPlayingReader`.
- `service/queue_service.go` — `Save`, `Resume`, `ResumeView`.
- `adapters/` — `handler/`, `persistence/`, `catalogbridge/`.

## Rules

- Construct a `QueueState` only through `newQueueState` — including rehydration from storage.
- Keep `TrackIds` as `[]string`; never wrap it in catalog's `TrackId`.
- Never let `TrackIds` or `NaturalOrder` be nil — `emptyIfNil` is the one home for that.
- Cap `TrackIds` and `NaturalOrder` at `MaxQueueLength`; `newQueueState` rejects longer input.
- Never reason over `NaturalOrder`; carry it through opaquely.
- Never import `net/http` from `domain/` — `ValidationError` carries a plain int status.
- `Resume` returns `EmptyQueueState`, never nil.
- Never grow queue logic here: advance/prev/shuffle/repeat live on the client.
- Pack and parse `source_id` only through `QueueSource`; the wire carries the structured `source`.
- Keep emitting `source_id` alongside `source` until every client reads the structured field.
- Every DB round trip derives its own deadline (`context.WithTimeout`) — the queue-state repo (`Upsert`, `GetForUser`) and the catalog bridge (`Lookup`) each bound the raw request context so a stuck dependency can never pin a pgx pool connection and drain the shared pool. The timeouts are package vars so tests can shrink them.
- `Upsert`'s `ON CONFLICT` carries `WHERE playback_queue_state.updated_at <= EXCLUDED.updated_at` — an older snapshot (delayed/retried/reordered save) must never clobber a newer stored row. `updated_at` is a server clock; a client-supplied monotonic version as the ordering key (ADR-0010) is the honest follow-up. `queue_state_repo_integration_test.go` (`//go:build integration`, DB-gated) reproduces the revert; the local `queue_state_repo_test.go` asserts the guard clause is present.
