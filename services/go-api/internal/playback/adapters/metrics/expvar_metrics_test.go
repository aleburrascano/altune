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

// The counter an operator alerts on is the one GET /admin/metrics/live reads,
// so the snapshot must carry the rate-limit rejections, not just expvar.
func TestReadSnapshot_ReportsRateLimitRejections(t *testing.T) {
	before := ReadSnapshot()

	NewExpvarPlaybackMetrics().QueueStateRateLimited()

	if after := ReadSnapshot(); after.QueueStateRateLimited != before.QueueStateRateLimited+1 {
		t.Errorf("queue_state_rate_limited_total = %d, want %d", after.QueueStateRateLimited, before.QueueStateRateLimited+1)
	}
}
