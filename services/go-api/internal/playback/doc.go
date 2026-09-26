// Package playback is the seam map for the playback module: a user's resumable
// queue state and the now-playing view over it. It holds no code; the module
// lives in the subpackages below, and this file only records where its
// cross-cutting seams run, so a change to one of them does not start by
// reading every file.
//
// # Layout
//
// Dependencies point inward: adapters depend on service, ports and domain;
// service depends on ports and domain; ports depends on domain; domain depends
// on nothing in the module (only internal/shared).
//
//   - domain: QueueState, QueuePosition, QueueSource, RepeatMode and the coded
//     409 conflicts (ErrStaleQueueWrite, ErrQueuePositionMismatch). No I/O.
//   - ports: QueueStateRepository and NowPlayingReader, the two QueueService
//     consumes, DeletedIdentityLister, which the erasure sweep consumes, plus
//     three metrics ports split by emitting adapter so a new counter ripples
//     through one consumer alone — EnrichmentMetrics (adapters/catalogbridge),
//     QueueStateMetrics (adapters/persistence) and RateLimitMetrics
//     (adapters/handler), each with a Noop default so its consumer works
//     without a metrics backend.
//   - service: QueueService (Save, SavePosition, Resume, ResumeView, Forget)
//     and ForgetDeletedIdentitiesService, the sweep that drives Forget for
//     accounts deleted out-of-band in Supabase.
//   - adapters/handler: the chi handlers for the /queue-state routes, plus the
//     per-user rate limiter in front of them.
//   - adapters/persistence: the pgx queue-state store, and the anti-join
//     against Supabase's auth.users that finds queue state whose owner is gone.
//   - adapters/catalogbridge: the catalog-backed now-playing reader.
//
// # The now-playing enrichment seam
//
// Resuming a queue enriches its current track from the catalog, a dependency
// that can be slow or down, so one degradation path crosses five layers and a
// change to any part of it (the failure threshold, the timeout, a counter, the
// wire contract) usually has to move the rest in step:
// adapters/catalogbridge/enrichment_breaker.go holds the breaker states, the
// consecutive-failure threshold and the open window;
// adapters/catalogbridge/now_playing_reader.go wraps Lookup in that breaker
// plus a per-call timeout and reports the failures the dependency owns;
// service/queue_service.go holds the enrichmentDisabled toggle and turns a
// failed lookup into ResumeView.CurrentTrackUnavailable while the resume still
// succeeds; ports/playback_metrics.go declares the degradation counters that
// adapters/metrics/expvar_metrics.go publishes; and
// adapters/handler/queue_handler.go surfaces the degraded state to clients as
// the current_track_unavailable response field. The toggle's kill switch is
// the PLAYBACK_NOW_PLAYING_ENRICHMENT_ENABLED environment variable, read
// outside this module in internal/app/playback_wiring.go, which is also where
// the reader, the service and the metrics adapter are wired together.
//
// # The queue-state fault seam
//
// The store behind a queue can hold a row the current domain can no longer
// rehydrate, and can stall, so a second degradation path crosses five files
// and a change to any part of it (the deadline, what counts as corrupt, a
// counter, what a poisoned row resumes as) usually has to move the rest in
// step: ports/queue_state_repo.go declares the ErrCorruptStoredState sentinel
// that GetForUser returns for a row that is present but invalid;
// adapters/persistence/queue_state_repo.go owns queueStateOpTimeout, decides
// which rows and which blown deadlines are the store's fault rather than the
// caller's, and reports both; ports/playback_metrics.go declares those two as
// QueueStateMetrics (CorruptStoredState, QueueStateOpTimedOut), which
// adapters/metrics/expvar_metrics.go publishes; and service/queue_service.go
// turns ErrCorruptStoredState into an empty queue state, so a poisoned row
// costs the user a resumable position instead of every future resume.
package playback
