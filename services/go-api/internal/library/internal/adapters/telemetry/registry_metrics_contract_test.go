package telemetry_test

import (
	librarytelemetry "altune/go-api/internal/library/internal/adapters/telemetry"
	"altune/go-api/internal/shared/telemetry"
	"testing"
)

func TestLibraryMetrics_CountIntoTheInjectedRegistryAndReadBackInTheSnapshot(t *testing.T) {
	reg := telemetry.NewRegistry()
	audio := librarytelemetry.NewAudioStoreMetrics(reg)
	db := librarytelemetry.NewDBCallMetrics(reg)

	audio.PresignFailed()
	audio.OrphanedDelete()
	audio.OrphanedDelete()
	audio.StreamRecoveryTriggered()
	audio.OrphanedAudioReconcileFailed()
	db.DBCallTimedOut()

	got := librarytelemetry.ReadSnapshot(reg)
	want := librarytelemetry.Snapshot{PresignFailures: 1, OrphanedDeletes: 2, StreamRecoveries: 1, OrphanedAudioReconcileFailures: 1, DBCallTimeouts: 1}
	if got != want {
		t.Fatalf("snapshot = %+v, want %+v", got, want)
	}
	if other := librarytelemetry.ReadSnapshot(telemetry.NewRegistry()); other != (librarytelemetry.Snapshot{}) {
		t.Fatalf("another registry saw %+v, want zeros: the metrics are not process globals", other)
	}
}
