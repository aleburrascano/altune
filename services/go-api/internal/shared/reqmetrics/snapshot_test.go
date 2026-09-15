package reqmetrics_test

import (
	"altune/go-api/internal/shared/reqmetrics"
	"testing"
	"time"
)

// TestReadSnapshot_ReflectsObserved records through the exported Observe (which
// writes the process-global registry) and reads it back through ReadSnapshot.
func TestReadSnapshot_ReflectsObserved(t *testing.T) {
	const route = "/v1/snapshot-probe/{id}"
	before := reqmetrics.ReadSnapshot().Routes[route].Count

	reqmetrics.Observe(route, 4*time.Millisecond)

	got, ok := reqmetrics.ReadSnapshot().Routes[route]
	if !ok {
		t.Fatalf("route %q missing from snapshot", route)
	}
	if got.Count != before+1 {
		t.Errorf("count = %d, want %d", got.Count, before+1)
	}
	if len(got.Buckets) == 0 {
		t.Fatal("route latency carries no buckets")
	}
	if last := got.Buckets[len(got.Buckets)-1]; last.LeMs != "+Inf" {
		t.Errorf("final bucket label = %q, want +Inf", last.LeMs)
	}
}

func TestObserve_EmptyRouteRecordsUnmatched(t *testing.T) {
	before := reqmetrics.ReadSnapshot().Routes["unmatched"].Count
	reqmetrics.Observe("", time.Millisecond)
	if got := reqmetrics.ReadSnapshot().Routes["unmatched"].Count; got != before+1 {
		t.Fatalf("unmatched count = %d, want %d", got, before+1)
	}
}
