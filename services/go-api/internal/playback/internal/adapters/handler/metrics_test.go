package handler_test

import (
	"altune/go-api/internal/playback/internal/adapters/handler"
	"altune/go-api/internal/shared/telemetry"
	"testing"
)

func TestPlaybackMetrics_EachSignalReachesItsSnapshotField(t *testing.T) {
	reg := telemetry.NewRegistry()
	m := handler.NewPlaybackMetrics(reg)

	m.EnrichmentFailed()
	m.CorruptStoredState()
	m.QueueStateOpTimedOut()
	m.QueueStateOpFailedTransient()
	m.QueueStateStaleWrite()
	m.NowPlayingLookupTimedOut()
	m.QueueStateRateLimited()
	m.EnrichmentBreakerRejected()
	m.SweepIdle()
	m.QueueStateErased(3)
	m.OwnerScanTimedOut()
	m.RetryOwnerDropped()

	want := handler.Snapshot{
		EnrichmentFailures: 1, CorruptStoredState: 1, QueueStateOpTimeouts: 1, QueueStateTransientFailures: 1,
		QueueStateStaleWrites: 1, NowPlayingLookupTimeouts: 1, QueueStateRateLimited: 1, EnrichmentBreakerRejections: 1,
		ErasureSweepIdle: 1, QueueStateErased: 3, ErasureOwnerScanTimeouts: 1, ErasureRetryOwnersDropped: 1,
	}
	if got := handler.ReadSnapshot(reg); got != want {
		t.Fatalf("snapshot = %+v, want %+v", got, want)
	}
}

func TestPlaybackMetrics_TrackBreakerOpenAndClosed(t *testing.T) {
	reg := telemetry.NewRegistry()
	m := handler.NewPlaybackMetrics(reg)

	m.EnrichmentBreakerOpened()
	if !handler.ReadSnapshot(reg).EnrichmentBreakerOpen {
		t.Error("now_playing_enrichment_breaker_open = false while the breaker is open, want true")
	}

	m.EnrichmentBreakerClosed()
	if handler.ReadSnapshot(reg).EnrichmentBreakerOpen {
		t.Error("now_playing_enrichment_breaker_open = true after the breaker closed, want false")
	}
}

func TestPlaybackMetrics_AreNotProcessGlobals(t *testing.T) {
	handler.NewPlaybackMetrics(telemetry.NewRegistry()).EnrichmentFailed()

	if other := handler.ReadSnapshot(telemetry.NewRegistry()); other != (handler.Snapshot{}) {
		t.Fatalf("another registry saw %+v, want zeros", other)
	}
}
