package metrics

import (
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
	m := NewExpvarPlaybackMetrics()

	cases := []struct {
		name    string
		varName string
		inc     func()
	}{
		{"enrichment failures", EnrichmentFailuresVar, m.EnrichmentFailed},
		{"corrupt stored state", CorruptStoredStateVar, m.CorruptStoredState},
		{"queue-state op timeouts", QueueStateOpTimeoutsVar, m.QueueStateOpTimedOut},
		{"now-playing lookup timeouts", NowPlayingLookupTimeoutsVar, m.NowPlayingLookupTimedOut},
		{"queue-state rate-limit rejections", QueueStateRateLimitedVar, m.QueueStateRateLimited},
		{"enrichment breaker rejections", EnrichmentBreakerRejectionsVar, m.EnrichmentBreakerRejected},
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

// The counter an operator alerts on is the one GET /observe/metrics/live reads,
// so the snapshot must carry the rate-limit rejections, not just expvar.
func TestReadSnapshot_ReportsRateLimitRejections(t *testing.T) {
	before := ReadSnapshot()

	NewExpvarPlaybackMetrics().QueueStateRateLimited()

	if after := ReadSnapshot(); after.QueueStateRateLimited != before.QueueStateRateLimited+1 {
		t.Errorf("queue_state_rate_limited_total = %d, want %d", after.QueueStateRateLimited, before.QueueStateRateLimited+1)
	}
}

// The breaker's state is a gauge, not a counter: the snapshot must answer "is
// enrichment fast-failing right now", so it has to fall back as well as rise.
func TestReadSnapshot_TracksBreakerOpenAndClosed(t *testing.T) {
	m := NewExpvarPlaybackMetrics()

	m.EnrichmentBreakerOpened()
	if !ReadSnapshot().EnrichmentBreakerOpen {
		t.Error("now_playing_enrichment_breaker_open = false while the breaker is open, want true")
	}

	m.EnrichmentBreakerClosed()
	if ReadSnapshot().EnrichmentBreakerOpen {
		t.Error("now_playing_enrichment_breaker_open = true after the breaker closed, want false")
	}
}

func TestReadSnapshot_ReportsErasureSweep(t *testing.T) {
	before := ReadSnapshot()
	m := NewExpvarPlaybackMetrics()

	m.SweepIdle()
	m.QueueStateErased(3)

	after := ReadSnapshot()
	if after.ErasureSweepIdle != before.ErasureSweepIdle+1 {
		t.Errorf("erasure_sweep_idle_total = %d, want %d", after.ErasureSweepIdle, before.ErasureSweepIdle+1)
	}
	if after.QueueStateErased != before.QueueStateErased+3 {
		t.Errorf("queue_state_erased_total = %d, want %d", after.QueueStateErased, before.QueueStateErased+3)
	}
}
