package metrics_test

import (
	"altune/go-api/internal/library/internal/adapters/metrics"
	"altune/go-api/internal/shared/telemetry"
	"testing"
)

func TestLibraryMetrics_CountIntoTheInjectedRegistryAndReadBackInTheSnapshot(t *testing.T) {
	reg := telemetry.NewRegistry()
	audio := metrics.NewAudioStoreMetrics(reg)
	db := metrics.NewDBCallMetrics(reg)

	audio.PresignFailed()
	audio.OrphanedDelete()
	audio.OrphanedDelete()
	audio.StreamRecoveryTriggered()
	audio.OrphanedAudioReconcileFailed()
	db.DBCallTimedOut()

	got := metrics.ReadSnapshot(reg)
	want := metrics.Snapshot{PresignFailures: 1, OrphanedDeletes: 2, StreamRecoveries: 1, OrphanedAudioReconcileFailures: 1, DBCallTimeouts: 1}
	if got != want {
		t.Fatalf("snapshot = %+v, want %+v", got, want)
	}
	if other := metrics.ReadSnapshot(telemetry.NewRegistry()); other != (metrics.Snapshot{}) {
		t.Fatalf("another registry saw %+v, want zeros: the metrics are not process globals", other)
	}
}
