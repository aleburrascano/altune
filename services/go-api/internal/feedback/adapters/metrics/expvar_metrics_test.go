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
	m.TrackerCreateFailed()
	if after := varValue(t, TrackerCreateFailuresVar); after != before+1 {
		t.Errorf("%s = %d, want %d after one increment", TrackerCreateFailuresVar, after, before+1)
	}
}
