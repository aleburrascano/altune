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

func TestExpvarFeedbackMetrics_PublishesAndIncrements(t *testing.T) {
	m := NewExpvarFeedbackMetrics()

	before := varValue(t, TrackerCreateFailuresVar)
	m.TrackerCreateFailed("tracker_unavailable")
	if after := varValue(t, TrackerCreateFailuresVar); after != before+1 {
		t.Errorf("%s = %d, want %d after one increment", TrackerCreateFailuresVar, after, before+1)
	}
}

// TestExpvarFeedbackMetrics_SplitsFailuresByCause pins that a dead token and an
// outage land in separate per-cause counters while both still move the total.
func TestExpvarFeedbackMetrics_SplitsFailuresByCause(t *testing.T) {
	if expvar.Get(TrackerCreateFailuresByCauseVar) == nil {
		t.Fatalf("expvar %q was never published", TrackerCreateFailuresByCauseVar)
	}
	m := NewExpvarFeedbackMetrics()
	before := ReadSnapshot()

	m.TrackerCreateFailed("tracker_unauthorized")
	m.TrackerCreateFailed("tracker_unavailable")
	m.TrackerCreateFailed("tracker_unavailable")

	after := ReadSnapshot()
	if after.TrackerCreateFailures != before.TrackerCreateFailures+3 {
		t.Errorf("total = %d, want %d", after.TrackerCreateFailures, before.TrackerCreateFailures+3)
	}
	for cause, delta := range map[string]int64{"tracker_unauthorized": 1, "tracker_unavailable": 2} {
		if got, want := after.TrackerCreateFailuresByCause[cause], before.TrackerCreateFailuresByCause[cause]+delta; got != want {
			t.Errorf("by cause[%s] = %d, want %d", cause, got, want)
		}
	}
}
