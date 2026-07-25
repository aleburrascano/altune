# Playback (server) context — router

Deliberately thin (ADR-0010): the live Queue is client-owned (`apps/mobile/src/shared/playback/`); this context persists only a snapshot.

Layout:

- `domain/queue_state.go` — `QueueState`, `QueueStateInput`, the `newQueueState` gate, `RepeatMode`, `ValidationError`.
- `ports/` — `QueueStateRepository`, `NowPlayingReader`.
- `service/queue_service.go` — `Save`, `Resume`, `ResumeView`.
- `adapters/` — `handler/`, `persistence/`, `catalogbridge/`.

## Rules

- Construct a `QueueState` only through `newQueueState` — including rehydration from storage.
- Keep `TrackIds` as `[]string`; never wrap it in catalog's `TrackId`.
- Never let `TrackIds` or `NaturalOrder` be nil — `emptyIfNil` is the one home for that.
- Never reason over `NaturalOrder`; carry it through opaquely.
- Never import `net/http` from `domain/` — `ValidationError` carries a plain int status.
- `Resume` returns `EmptyQueueState`, never nil.
- Never grow queue logic here: advance/prev/shuffle/repeat live on the client.

Why each rule exists: `okf/backend/playback.md`; table in `okf/data/playback-queue-state-table.md` — read before structural work; update in the same commit when behavior they describe changes (pre-commit hook enforces).
