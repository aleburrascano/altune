package ports

type AudioStoreMetrics interface {
	PresignFailed()
	OrphanedDelete()
	StreamRecoveryTriggered()
	OrphanedAudioReconcileFailed()
}

func NoopAudioStoreMetrics() AudioStoreMetrics { return noopAudioStoreMetrics{} }

type noopAudioStoreMetrics struct{}

func (noopAudioStoreMetrics) PresignFailed()                {}
func (noopAudioStoreMetrics) OrphanedDelete()               {}
func (noopAudioStoreMetrics) StreamRecoveryTriggered()      {}
func (noopAudioStoreMetrics) OrphanedAudioReconcileFailed() {}
