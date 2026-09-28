package catalogtest

import "altune/go-api/internal/catalog/ports"

type Metrics struct {
	PresignFailures                int
	OrphanedDeletes                int
	StreamRecoveries               int
	OrphanedAudioReconcileFailures int
}

var _ ports.AudioStoreMetrics = (*Metrics)(nil)

func (m *Metrics) PresignFailed()           { m.PresignFailures++ }
func (m *Metrics) OrphanedDelete()          { m.OrphanedDeletes++ }
func (m *Metrics) StreamRecoveryTriggered() { m.StreamRecoveries++ }

func (m *Metrics) OrphanedAudioReconcileFailed() { m.OrphanedAudioReconcileFailures++ }
