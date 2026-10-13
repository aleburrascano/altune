# Playback — Module Architecture

The playback bounded context (`services/go-api/internal/playback/`) persists one
queue snapshot per `UserId` so a client can resume where it left off after reopening
the app, and enriches the snapshot's current track for the now-playing view. The
queue itself is client-managed; the server stores the latest snapshot and never
drives playback. This document is the module map: the layers, the seams, and the
invariants a change must preserve.

---

## 0. Orientation

Playback owns one aggregate, `domain.QueueState` (tracks, current index, shuffle,
repeat mode, source) and its `QueuePosition` update. It is a **customer of catalog**
for now-playing enrichment and of **auth** for the request user, and reaches both
only through bridges.

## 1. Layer map

| Layer | File | Job |
|---|---|---|
| domain | `queue_state.go`, `queue_position.go`, `queue_source.go`, `repeat_mode.go` | Snapshot, position update, source and repeat value types with their validation |
| domain | `queue_validation_error.go` | `QueueValidationError`, `newValidationError`, the `playback.*` validation codes |
| domain | `stale_write_error.go` | `StaleWriteError`, `ErrStaleQueueWrite`, `ErrQueuePositionMismatch` (409) |
| domain | `unavailable_error.go` | `UnavailableError`, `ErrQueueStateUnavailable` (503, `Retry-After: 1`) |
| domain | `queue_rate_limited_error.go` | `QueueRateLimitedError` (429, `playback.rate_limited`) |
| ports | `queue_state.go` | `QueueStateRepository`, `ErrCorruptStoredState`, and the `QueueStateMetrics` and `RateLimitMetrics` interfaces with their noops |
| ports | `now_playing.go` | `NowPlayingReader`, `NowPlayingTrack`, and the `EnrichmentMetrics` interface with its noop |
| ports | `erasure.go` | `DeletedIdentityLister`, `OwnerScanPage`, `ErrIdentityStoreUnavailable`, `ErasedStateReaper`, and the `ErasureSweepMetrics` interface with its noop |
| service | `queue/service.go` | Save, update position, get, enrich the current track |
| service | `erasure/sweeper.go` | Erasure sweep |
| adapters/handler | `queue_handler.go` | HTTP routes for the queue snapshot |
| adapters/handler | `queue_state_rate_limit.go` | Per-user queue-state rate limiter middleware |
| adapters/authbridge | `require_user_id.go` | Request user from `internal/auth` |
| adapters/catalogbridge | `now_playing_reader.go`, `enrichment_breaker.go` | Catalog lookup behind a circuit breaker |
| adapters/metrics | `expvar_playback_metrics.go` | expvar implementation of the metrics ports |
| adapters/persistence | `pgx_queue_state_repository.go`, `pgx_deleted_identity_repository.go`, `op_faults.go` | Postgres storage and error classification |

## 2. Now-playing enrichment

`adapters/catalogbridge` reads the current track from catalog. A breaker opens after
5 consecutive dependency failures and stays open for 30s, then admits one probe
(half-open). Open means the lookup returns an enrichment-unavailable error; the
snapshot itself still loads. Opening and half-open log at WARN, closing at INFO.

## 3. Queue-state rate limit

Queue-state requests are limited per user: one token every 2s, burst 20
(`DefaultQueueStateRateLimit`). Over the limit the handler answers 429 with
`Retry-After`. Requests without a user in context pass through to the auth 401.

## 4. Erasure sweep

`service/erasure/sweeper.go` lists owners whose identity no longer
exists and deletes their queue state, so a deleted account leaves no snapshot behind.
An unavailable identity store ends the sweep idle and is counted, not treated as
"nobody to erase".

## 5. Invariants

- A stale write or a position that does not match the stored queue is rejected with 409; the stored snapshot is unchanged.
- A storage outage maps to `UnavailableError`: 503 with `Retry-After: 1`.
- A corrupt stored snapshot is `ErrCorruptStoredState`, never served as valid state.
- Every error a caller branches on is declared in `domain/` and written through `httputil.HandleServiceError`.
- One snapshot per `UserId`; the sweep is the only path that removes it for an account that no longer exists.
