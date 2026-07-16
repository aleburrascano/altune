# Playback (server) context — router

Deliberately thin (ADR-0010): the live Queue is client-owned (`apps/mobile/src/shared/playback/`); this context persists only a snapshot — the server half of the resume-on-reopen Memento.

Invariants:

- All `QueueState` construction funnels through the unexported `newQueueState` gate — including rehydration from storage, so a corrupt row can't produce an invalid state.
- `TrackIds` is deliberately `[]string`, not catalog's `TrackId` (AIDEV-DECISION: id-by-string across the context seam).
- `Resume` returns `EmptyQueueState`, never nil — handlers need no nil-check.
- Don't grow queue logic here: advance/prev/shuffle/repeat semantics live on the client.

Knowledge base: `okf/backend/playback.md`; table in `okf/data/playback-queue-state-table.md` — read before structural work; update in the same commit when behavior they describe changes (pre-commit hook enforces).
