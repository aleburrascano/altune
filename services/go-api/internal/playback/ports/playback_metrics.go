package ports

// PlaybackMetrics aggregates counters that reveal playback's known degradable
// failure modes, so an operator can dashboard or alert on them instead of
// correlating scattered log lines by hand. Each method is a health signal for
// a mode that today degrades gracefully (a resume still returns) but otherwise
// leaves no counter behind.
type PlaybackMetrics interface {
	// EnrichmentFailed records one now-playing enrichment lookup that failed
	// for a reason owned by the catalog dependency (not a client cancel). A
	// sustained rise means catalog is degrading.
	EnrichmentFailed()
	// CorruptStoredState records one stored queue-state row that exists but can
	// no longer be rehydrated into a valid domain.QueueState.
	CorruptStoredState()
	// QueueStateOpTimedOut records one queue-state database operation that
	// exceeded its per-op deadline (queueStateOpTimeout).
	QueueStateOpTimedOut()
	// NowPlayingLookupTimedOut records one now-playing catalog lookup that
	// exceeded its per-call deadline (nowPlayingLookupTimeout).
	NowPlayingLookupTimedOut()
}

// NoopPlaybackMetrics returns a PlaybackMetrics that records nothing. It is the
// default so services and adapters stay usable without a metrics backend wired
// in.
func NoopPlaybackMetrics() PlaybackMetrics { return noopPlaybackMetrics{} }

type noopPlaybackMetrics struct{}

func (noopPlaybackMetrics) EnrichmentFailed()         {}
func (noopPlaybackMetrics) CorruptStoredState()       {}
func (noopPlaybackMetrics) QueueStateOpTimedOut()     {}
func (noopPlaybackMetrics) NowPlayingLookupTimedOut() {}
