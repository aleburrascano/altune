# Playback (server) context — router

Deliberately thin (ADR-0010): the live Queue is client-owned (`apps/mobile/src/shared/playback/`); this context persists only a snapshot — the server half of the resume-on-reopen Memento.

Invariants:

- All `QueueState` construction funnels through the unexported `newQueueState` gate — including rehydration from storage, so a corrupt row can't produce an invalid state.
- `TrackIds` is deliberately `[]string`, not catalog's `TrackId`. Catalog owns `TrackId` identity; playback references those tracks by id across the context seam (reference-by-id). Wrapping them here would couple playback to catalog for a snapshot the server never reasons over.
- `emptyIfNil` is the one home for the never-nil invariant on `TrackIds` / `NaturalOrder`, so callers and JSON serialization can rely on it against NULL rows and omitted JSON fields.
- `NaturalOrder` is the pre-shuffle order and is opaque to the server — carried through, never reasoned over. It lets the client rebuild the shuffled sequence and un-shuffle back after relaunch; empty for older rows and clients that don't send it.
- `domain.ValidationError` carries a plain `int` status rather than a `net/http` constant so the domain layer stays free of `net/http`; it structurally satisfies `httputil.StatusError`, so handlers map it to 400 instead of a blanket 500.
- `Resume` returns `EmptyQueueState`, never nil — handlers need no nil-check. `EmptyQueueState` still routes through the `newQueueState` gate and panics on error, since empty input cannot fail validation.
- Don't grow queue logic here: advance/prev/shuffle/repeat semantics live on the client.

Knowledge base: `okf/backend/playback.md`; table in `okf/data/playback-queue-state-table.md` — read before structural work; update in the same commit when behavior they describe changes (pre-commit hook enforces).
