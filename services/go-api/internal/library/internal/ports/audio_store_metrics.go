package ports

type AudioStoreMetrics interface {
	PresignFailed()
	OrphanedDelete()
	StreamRecoveryTriggered()
	OrphanedAudioReconcileFailed()
}
