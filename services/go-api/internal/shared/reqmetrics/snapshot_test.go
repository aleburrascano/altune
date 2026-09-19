package reqmetrics_test

import (
	"altune/go-api/internal/shared/reqmetrics"
	"encoding/json"
	"testing"
	"time"
)

// TestReadSnapshot_ReflectsObserved records through the exported Observe (which
// writes the process-global registry) and reads it back through ReadSnapshot.
func TestReadSnapshot_ReflectsObserved(t *testing.T) {
	const route = "/v1/snapshot-probe/{id}"
	before := reqmetrics.ReadSnapshot().Routes[route].Count

	reqmetrics.Observe(route, 4*time.Millisecond, 200)

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
	reqmetrics.Observe("", time.Millisecond, 404)
	if got := reqmetrics.ReadSnapshot().Routes["unmatched"].Count; got != before+1 {
		t.Fatalf("unmatched count = %d, want %d", got, before+1)
	}
}

// TestReadSnapshot_TalliesStatusClassesPerRoute proves each observed status
// lands in its own 2xx/4xx/5xx slot on the route it belongs to.
func TestReadSnapshot_TalliesStatusClassesPerRoute(t *testing.T) {
	const route = "/v1/status-probe/{id}"
	before := reqmetrics.ReadSnapshot().Routes[route].Status

	reqmetrics.Observe(route, time.Millisecond, 200)
	reqmetrics.Observe(route, time.Millisecond, 404)
	reqmetrics.Observe(route, time.Millisecond, 502)

	got := reqmetrics.ReadSnapshot().Routes[route].Status
	if got.Count2xx != before.Count2xx+1 {
		t.Errorf("2xx = %d, want %d", got.Count2xx, before.Count2xx+1)
	}
	if got.Count4xx != before.Count4xx+1 {
		t.Errorf("4xx = %d, want %d", got.Count4xx, before.Count4xx+1)
	}
	if got.Count5xx != before.Count5xx+1 {
		t.Errorf("5xx = %d, want %d", got.Count5xx, before.Count5xx+1)
	}
}

// TestRouteLatency_StatusSerializesUnderStatusKey pins the seam contract overseer
// reads: the status classes serialize as {"2xx":..,"4xx":..,"5xx":..} under
// "status" on each route.
func TestRouteLatency_StatusSerializesUnderStatusKey(t *testing.T) {
	route := reqmetrics.RouteLatency{Status: reqmetrics.StatusClasses{Count2xx: 7, Count4xx: 3, Count5xx: 1}}

	raw, err := json.Marshal(route)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var shape struct {
		Status map[string]uint64 `json:"status"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := map[string]uint64{"2xx": 7, "4xx": 3, "5xx": 1}
	for class, n := range want {
		if shape.Status[class] != n {
			t.Errorf("status[%q] = %d, want %d", class, shape.Status[class], n)
		}
	}
}
