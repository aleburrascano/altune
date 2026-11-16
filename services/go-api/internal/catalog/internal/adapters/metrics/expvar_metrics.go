package metrics

import (
	"altune/go-api/internal/catalog/ports"
	"expvar"
)

const (
	PresignFailuresVar                = "catalog_audio_presign_failures_total"
	OrphanedDeletesVar                = "catalog_audio_orphaned_deletes_total"
	StreamRecoveriesVar               = "catalog_audio_stream_recoveries_total"
	OrphanedAudioReconcileFailuresVar = "catalog_orphaned_audio_reconcile_failures_total"
	DBCallTimeoutsVar                 = "catalog_db_call_timeouts_total"
)

var (
	presignFailures                = expvar.NewInt(PresignFailuresVar)
	orphanedDeletes                = expvar.NewInt(OrphanedDeletesVar)
	streamRecoveries               = expvar.NewInt(StreamRecoveriesVar)
	orphanedAudioReconcileFailures = expvar.NewInt(OrphanedAudioReconcileFailuresVar)
	dbCallTimeouts                 = expvar.NewInt(DBCallTimeoutsVar)
)

type ExpvarAudioStoreMetrics struct{}

var _ ports.AudioStoreMetrics = ExpvarAudioStoreMetrics{}

func NewExpvarAudioStoreMetrics() ExpvarAudioStoreMetrics { return ExpvarAudioStoreMetrics{} }

func (ExpvarAudioStoreMetrics) PresignFailed()           { presignFailures.Add(1) }
func (ExpvarAudioStoreMetrics) OrphanedDelete()          { orphanedDeletes.Add(1) }
func (ExpvarAudioStoreMetrics) StreamRecoveryTriggered() { streamRecoveries.Add(1) }

func (ExpvarAudioStoreMetrics) OrphanedAudioReconcileFailed() {
	orphanedAudioReconcileFailures.Add(1)
}

type ExpvarDBCallMetrics struct{}

var _ ports.DBCallMetrics = ExpvarDBCallMetrics{}

func NewExpvarDBCallMetrics() ExpvarDBCallMetrics { return ExpvarDBCallMetrics{} }

func (ExpvarDBCallMetrics) DBCallTimedOut() { dbCallTimeouts.Add(1) }

type Snapshot struct {
	PresignFailures                int64 `json:"presign_failures_total"`
	OrphanedDeletes                int64 `json:"orphaned_deletes_total"`
	StreamRecoveries               int64 `json:"stream_recoveries_total"`
	OrphanedAudioReconcileFailures int64 `json:"orphaned_audio_reconcile_failures_total"`
	DBCallTimeouts                 int64 `json:"db_call_timeouts_total"`
}

func ReadSnapshot() Snapshot {
	return Snapshot{
		PresignFailures:                presignFailures.Value(),
		OrphanedDeletes:                orphanedDeletes.Value(),
		StreamRecoveries:               streamRecoveries.Value(),
		OrphanedAudioReconcileFailures: orphanedAudioReconcileFailures.Value(),
		DBCallTimeouts:                 dbCallTimeouts.Value(),
	}
}
