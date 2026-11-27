package metrics

import (
	"altune/go-api/internal/library/internal/core/ports"
	"altune/go-api/internal/shared/telemetry"
)

const (
	PresignFailuresVar                = "catalog_audio_presign_failures_total"
	OrphanedDeletesVar                = "catalog_audio_orphaned_deletes_total"
	StreamRecoveriesVar               = "catalog_audio_stream_recoveries_total"
	OrphanedAudioReconcileFailuresVar = "catalog_orphaned_audio_reconcile_failures_total"
	DBCallTimeoutsVar                 = "catalog_db_call_timeouts_total"
)

type RegistryAudioStoreMetrics struct {
	presignFailures                *telemetry.Counter
	orphanedDeletes                *telemetry.Counter
	streamRecoveries               *telemetry.Counter
	orphanedAudioReconcileFailures *telemetry.Counter
}

var _ ports.AudioStoreMetrics = RegistryAudioStoreMetrics{}

func NewAudioStoreMetrics(reg *telemetry.Registry) RegistryAudioStoreMetrics {
	return RegistryAudioStoreMetrics{
		presignFailures:                reg.Counter(PresignFailuresVar),
		orphanedDeletes:                reg.Counter(OrphanedDeletesVar),
		streamRecoveries:               reg.Counter(StreamRecoveriesVar),
		orphanedAudioReconcileFailures: reg.Counter(OrphanedAudioReconcileFailuresVar),
	}
}

func (m RegistryAudioStoreMetrics) PresignFailed()           { m.presignFailures.Add(1) }
func (m RegistryAudioStoreMetrics) OrphanedDelete()          { m.orphanedDeletes.Add(1) }
func (m RegistryAudioStoreMetrics) StreamRecoveryTriggered() { m.streamRecoveries.Add(1) }

func (m RegistryAudioStoreMetrics) OrphanedAudioReconcileFailed() {
	m.orphanedAudioReconcileFailures.Add(1)
}

type RegistryDBCallMetrics struct {
	dbCallTimeouts *telemetry.Counter
}

var _ ports.DBCallMetrics = RegistryDBCallMetrics{}

func NewDBCallMetrics(reg *telemetry.Registry) RegistryDBCallMetrics {
	return RegistryDBCallMetrics{dbCallTimeouts: reg.Counter(DBCallTimeoutsVar)}
}

func (m RegistryDBCallMetrics) DBCallTimedOut() { m.dbCallTimeouts.Add(1) }

type Snapshot struct {
	PresignFailures                int64 `json:"presign_failures_total"`
	OrphanedDeletes                int64 `json:"orphaned_deletes_total"`
	StreamRecoveries               int64 `json:"stream_recoveries_total"`
	OrphanedAudioReconcileFailures int64 `json:"orphaned_audio_reconcile_failures_total"`
	DBCallTimeouts                 int64 `json:"db_call_timeouts_total"`
}

func ReadSnapshot(reg *telemetry.Registry) Snapshot {
	return Snapshot{
		PresignFailures:                reg.Counter(PresignFailuresVar).Value(),
		OrphanedDeletes:                reg.Counter(OrphanedDeletesVar).Value(),
		StreamRecoveries:               reg.Counter(StreamRecoveriesVar).Value(),
		OrphanedAudioReconcileFailures: reg.Counter(OrphanedAudioReconcileFailuresVar).Value(),
		DBCallTimeouts:                 reg.Counter(DBCallTimeoutsVar).Value(),
	}
}
