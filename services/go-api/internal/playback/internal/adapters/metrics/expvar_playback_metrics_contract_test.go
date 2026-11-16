package metrics_test

import (
	"altune/go-api/internal/playback/internal/adapters/metrics"
	"expvar"
	"strconv"
	"testing"
)

func varValue(t *testing.T, name string) int64 {
	t.Helper()
	v := expvar.Get(name)
	if v == nil {
		t.Fatalf("expvar %q was never published", name)
	}
	n, err := strconv.ParseInt(v.String(), 10, 64)
	if err != nil {
		t.Fatalf("expvar %q = %q, not an integer: %v", name, v.String(), err)
	}
	return n
}

func TestExpvarPlaybackMetrics_PublishesAndIncrements(t *testing.T) {
	m := metrics.NewExpvarPlaybackMetrics()

	cases := []struct {
		name    string
		varName string
		inc     func()
	}{
		{"enrichment failures", metrics.EnrichmentFailuresVar, m.EnrichmentFailed},
		{"corrupt stored state", metrics.CorruptStoredStateVar, m.CorruptStoredState},
		{"queue-state op timeouts", metrics.QueueStateOpTimeoutsVar, m.QueueStateOpTimedOut},
		{"queue-state transient failures", metrics.QueueStateTransientFailuresVar, m.QueueStateOpFailedTransient},
		{"queue-state stale writes", metrics.QueueStateStaleWritesVar, m.QueueStateStaleWrite},
		{"now-playing lookup timeouts", metrics.NowPlayingLookupTimeoutsVar, m.NowPlayingLookupTimedOut},
		{"queue-state rate-limit rejections", metrics.QueueStateRateLimitedVar, m.QueueStateRateLimited},
		{"enrichment breaker rejections", metrics.EnrichmentBreakerRejectionsVar, m.EnrichmentBreakerRejected},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := varValue(t, tc.varName)
			tc.inc()
			if after := varValue(t, tc.varName); after != before+1 {
				t.Errorf("%s = %d, want %d after one increment", tc.varName, after, before+1)
			}
		})
	}
}

func TestReadSnapshot_ReportsRateLimitRejections(t *testing.T) {
	before := metrics.ReadSnapshot()

	metrics.NewExpvarPlaybackMetrics().QueueStateRateLimited()

	if after := metrics.ReadSnapshot(); after.QueueStateRateLimited != before.QueueStateRateLimited+1 {
		t.Errorf("queue_state_rate_limited_total = %d, want %d", after.QueueStateRateLimited, before.QueueStateRateLimited+1)
	}
}

func TestReadSnapshot_TracksBreakerOpenAndClosed(t *testing.T) {
	m := metrics.NewExpvarPlaybackMetrics()

	m.EnrichmentBreakerOpened()
	if !metrics.ReadSnapshot().EnrichmentBreakerOpen {
		t.Error("now_playing_enrichment_breaker_open = false while the breaker is open, want true")
	}

	m.EnrichmentBreakerClosed()
	if metrics.ReadSnapshot().EnrichmentBreakerOpen {
		t.Error("now_playing_enrichment_breaker_open = true after the breaker closed, want false")
	}
}

func TestReadSnapshot_ReportsErasureSweep(t *testing.T) {
	before := metrics.ReadSnapshot()
	m := metrics.NewExpvarPlaybackMetrics()

	m.SweepIdle()
	m.QueueStateErased(3)
	m.OwnerScanTimedOut()
	m.RetryOwnerDropped()

	after := metrics.ReadSnapshot()
	if after.ErasureRetryOwnersDropped != before.ErasureRetryOwnersDropped+1 {
		t.Errorf("erasure_retry_owners_dropped_total = %d, want %d", after.ErasureRetryOwnersDropped, before.ErasureRetryOwnersDropped+1)
	}
	if after.ErasureOwnerScanTimeouts != before.ErasureOwnerScanTimeouts+1 {
		t.Errorf("erasure_owner_scan_timeouts_total = %d, want %d", after.ErasureOwnerScanTimeouts, before.ErasureOwnerScanTimeouts+1)
	}
	if after.ErasureSweepIdle != before.ErasureSweepIdle+1 {
		t.Errorf("erasure_sweep_idle_total = %d, want %d", after.ErasureSweepIdle, before.ErasureSweepIdle+1)
	}
	if after.QueueStateErased != before.QueueStateErased+3 {
		t.Errorf("queue_state_erased_total = %d, want %d", after.QueueStateErased, before.QueueStateErased+3)
	}
}

func TestReadSnapshot_ReportsQueueStateFailures(t *testing.T) {
	before := metrics.ReadSnapshot()

	m := metrics.NewExpvarPlaybackMetrics()
	m.QueueStateOpFailedTransient()
	m.QueueStateStaleWrite()

	after := metrics.ReadSnapshot()
	if after.QueueStateTransientFailures != before.QueueStateTransientFailures+1 {
		t.Errorf("queue_state_op_transient_failures_total = %d, want %d", after.QueueStateTransientFailures, before.QueueStateTransientFailures+1)
	}
	if after.QueueStateStaleWrites != before.QueueStateStaleWrites+1 {
		t.Errorf("queue_state_stale_writes_total = %d, want %d", after.QueueStateStaleWrites, before.QueueStateStaleWrites+1)
	}
}
