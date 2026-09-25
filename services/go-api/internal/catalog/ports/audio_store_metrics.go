package ports

// AudioStoreMetrics aggregates counters that reveal degradation on the
// audio-store surface, so an operator can dashboard or alert on them instead
// of correlating scattered log lines by hand.
type AudioStoreMetrics interface {
	// PresignFailed records one failed attempt to presign an audio URL.
	PresignFailed()
	// OrphanedDelete records one audio object left behind in storage after its
	// track row was deleted (the storage delete failed).
	OrphanedDelete()
	// StreamRecoveryTriggered records one stream that fell back to recovery
	// because its audio object was missing from storage at read time.
	StreamRecoveryTriggered()
	// OrphanedAudioReconcileFailed records one queued orphan the reconcile
	// sweep could not delete. The sweep retries a failed key forever, so a
	// rising count is the backlog of keys storage refuses to release.
	OrphanedAudioReconcileFailed()
}

// NoopAudioStoreMetrics returns an AudioStoreMetrics that records nothing. It
// is the default so services stay usable without a metrics backend wired in.
func NoopAudioStoreMetrics() AudioStoreMetrics { return noopAudioStoreMetrics{} }

type noopAudioStoreMetrics struct{}

func (noopAudioStoreMetrics) PresignFailed()                {}
func (noopAudioStoreMetrics) OrphanedDelete()               {}
func (noopAudioStoreMetrics) StreamRecoveryTriggered()      {}
func (noopAudioStoreMetrics) OrphanedAudioReconcileFailed() {}
