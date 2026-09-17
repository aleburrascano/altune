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

// NoopEnrichmentMetrics returns an EnrichmentMetrics that records nothing. It
// is the default so services and adapters stay usable without a metrics backend
// wired in.
func NoopEnrichmentMetrics() EnrichmentMetrics { return noopEnrichmentMetrics{} }

// NoopQueueStateMetrics returns a QueueStateMetrics that records nothing, for
// the same reason as NoopEnrichmentMetrics.
func NoopQueueStateMetrics() QueueStateMetrics { return noopQueueStateMetrics{} }

type noopEnrichmentMetrics struct{}

func (noopEnrichmentMetrics) EnrichmentFailed()         {}
func (noopEnrichmentMetrics) NowPlayingLookupTimedOut() {}

type noopQueueStateMetrics struct{}

func (noopQueueStateMetrics) CorruptStoredState()   {}
func (noopQueueStateMetrics) QueueStateOpTimedOut() {}
