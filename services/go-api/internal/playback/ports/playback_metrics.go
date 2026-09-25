package ports

// Playback's degradation counters are split by emitting adapter rather than
// gathered into one sink, so an adapter's dependency names only the signals it
// owns and a new counter ripples through that consumer alone. Each counter is a
// health signal for a mode that degrades gracefully (a resume still returns)
// and would otherwise leave nothing behind but scattered log lines.

// EnrichmentMetrics counts the now-playing enrichment path's degradations.
type EnrichmentMetrics interface {
	// EnrichmentFailed records one now-playing enrichment lookup that failed
	// for a reason owned by the catalog dependency (not a client cancel). A
	// sustained rise means catalog is degrading.
	EnrichmentFailed()
	// NowPlayingLookupTimedOut records one now-playing catalog lookup that
	// exceeded its per-call deadline (nowPlayingLookupTimeout).
	NowPlayingLookupTimedOut()
	// EnrichmentBreakerRejected records one lookup the fast-fail breaker
	// refused outright. No dependency call is attempted on that path, so this
	// is the only counter that keeps climbing while the breaker is degraded —
	// EnrichmentFailed goes flat exactly when the outage is worst.
	EnrichmentBreakerRejected()
	// EnrichmentBreakerOpened records the breaker entering its degraded state.
	// It is a gauge edge, not a counter: degraded until EnrichmentBreakerClosed.
	EnrichmentBreakerOpened()
	// EnrichmentBreakerClosed records a recovery probe succeeding, which is the
	// only way out of the degraded state.
	EnrichmentBreakerClosed()
}

// QueueStateMetrics counts the queue-state store's degradations.
type QueueStateMetrics interface {
	// CorruptStoredState records one stored queue-state row that exists but can
	// no longer be rehydrated into a valid domain.QueueState.
	CorruptStoredState()
	// QueueStateOpTimedOut records one queue-state database operation that
	// exceeded its per-op deadline (queueStateOpTimeout).
	QueueStateOpTimedOut()
}

// RateLimitMetrics counts the queue-state HTTP surface's refusals.
type RateLimitMetrics interface {
	// QueueStateRateLimited records one /queue-state request refused by the
	// per-user rate limiter (429). A sustained rise is a client retry loop, a
	// misconfigured device, or abuse, none of which leave any other trace on
	// this instance.
	QueueStateRateLimited()
}

type ErasureSweepMetrics interface {
	SweepIdle()
	QueueStateErased(n int)
}

// NoopEnrichmentMetrics returns an EnrichmentMetrics that records nothing. It
// is the default so services and adapters stay usable without a metrics backend
// wired in.
func NoopEnrichmentMetrics() EnrichmentMetrics { return noopEnrichmentMetrics{} }

// NoopQueueStateMetrics returns a QueueStateMetrics that records nothing, for
// the same reason as NoopEnrichmentMetrics.
func NoopQueueStateMetrics() QueueStateMetrics { return noopQueueStateMetrics{} }

// NoopRateLimitMetrics returns a RateLimitMetrics that records nothing, for the
// same reason as NoopEnrichmentMetrics.
func NoopRateLimitMetrics() RateLimitMetrics { return noopRateLimitMetrics{} }

func NoopErasureSweepMetrics() ErasureSweepMetrics { return noopErasureSweepMetrics{} }

type noopErasureSweepMetrics struct{}

func (noopErasureSweepMetrics) SweepIdle()           {}
func (noopErasureSweepMetrics) QueueStateErased(int) {}

type noopEnrichmentMetrics struct{}

func (noopEnrichmentMetrics) EnrichmentFailed()          {}
func (noopEnrichmentMetrics) NowPlayingLookupTimedOut()  {}
func (noopEnrichmentMetrics) EnrichmentBreakerRejected() {}
func (noopEnrichmentMetrics) EnrichmentBreakerOpened()   {}
func (noopEnrichmentMetrics) EnrichmentBreakerClosed()   {}

type noopQueueStateMetrics struct{}

func (noopQueueStateMetrics) CorruptStoredState()   {}
func (noopQueueStateMetrics) QueueStateOpTimedOut() {}

type noopRateLimitMetrics struct{}

func (noopRateLimitMetrics) QueueStateRateLimited() {}
