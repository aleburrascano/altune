// Package metrics provides expvar-backed implementations of the catalog
// audio-store and database-call metrics ports. expvar is stdlib, so it adds no
// dependency: the counters are process-global published integers, exposed to
// operators through the operator-only GET /admin/metrics/live route (no public
// /debug/vars handler is mounted).
package metrics

import (
	"altune/go-api/internal/catalog/ports"
	"expvar"
)

// Published expvar variable names for the catalog degradation counters.
const (
	PresignFailuresVar                = "catalog_audio_presign_failures_total"
	OrphanedDeletesVar                = "catalog_audio_orphaned_deletes_total"
	StreamRecoveriesVar               = "catalog_audio_stream_recoveries_total"
	OrphanedAudioReconcileFailuresVar = "catalog_orphaned_audio_reconcile_failures_total"
	DBCallTimeoutsVar                 = "catalog_db_call_timeouts_total"
)

// Declared at package scope because expvar.NewInt panics on a duplicate name;
// registering once keeps the adapter safe to construct any number of times.
var (
	presignFailures                = expvar.NewInt(PresignFailuresVar)
	orphanedDeletes                = expvar.NewInt(OrphanedDeletesVar)
	streamRecoveries               = expvar.NewInt(StreamRecoveriesVar)
	orphanedAudioReconcileFailures = expvar.NewInt(OrphanedAudioReconcileFailuresVar)
	dbCallTimeouts                 = expvar.NewInt(DBCallTimeoutsVar)
)

// ExpvarAudioStoreMetrics implements ports.AudioStoreMetrics by incrementing
// process-global expvar counters.
type ExpvarAudioStoreMetrics struct{}

var _ ports.AudioStoreMetrics = ExpvarAudioStoreMetrics{}

// NewExpvarAudioStoreMetrics returns an ExpvarAudioStoreMetrics.
func NewExpvarAudioStoreMetrics() ExpvarAudioStoreMetrics { return ExpvarAudioStoreMetrics{} }

func (ExpvarAudioStoreMetrics) PresignFailed()           { presignFailures.Add(1) }
func (ExpvarAudioStoreMetrics) OrphanedDelete()          { orphanedDeletes.Add(1) }
func (ExpvarAudioStoreMetrics) StreamRecoveryTriggered() { streamRecoveries.Add(1) }

func (ExpvarAudioStoreMetrics) OrphanedAudioReconcileFailed() {
	orphanedAudioReconcileFailures.Add(1)
}

// ExpvarDBCallMetrics implements ports.DBCallMetrics by incrementing
// process-global expvar counters.
type ExpvarDBCallMetrics struct{}

var _ ports.DBCallMetrics = ExpvarDBCallMetrics{}

// NewExpvarDBCallMetrics returns an ExpvarDBCallMetrics.
func NewExpvarDBCallMetrics() ExpvarDBCallMetrics { return ExpvarDBCallMetrics{} }

func (ExpvarDBCallMetrics) DBCallTimedOut() { dbCallTimeouts.Add(1) }

// Snapshot is a point-in-time read of the catalog audio-store and database-call
// degradation counters, shaped for JSON exposure.
type Snapshot struct {
	PresignFailures                int64 `json:"presign_failures_total"`
	OrphanedDeletes                int64 `json:"orphaned_deletes_total"`
	StreamRecoveries               int64 `json:"stream_recoveries_total"`
	OrphanedAudioReconcileFailures int64 `json:"orphaned_audio_reconcile_failures_total"`
	DBCallTimeouts                 int64 `json:"db_call_timeouts_total"`
}

// ReadSnapshot returns the current values of the published catalog counters. It
// is a read-only accessor over the package-scope expvar vars so callers can
// expose these specific counters without reaching the raw expvar registry (which
// also publishes process globals like cmdline and memstats).
func ReadSnapshot() Snapshot {
	return Snapshot{
		PresignFailures:                presignFailures.Value(),
		OrphanedDeletes:                orphanedDeletes.Value(),
		StreamRecoveries:               streamRecoveries.Value(),
		OrphanedAudioReconcileFailures: orphanedAudioReconcileFailures.Value(),
		DBCallTimeouts:                 dbCallTimeouts.Value(),
	}
}
