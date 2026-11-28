package requestmetrics_test

import (
	"altune/go-api/internal/platform/requestmetrics"
	"altune/go-api/internal/platform/requestmetrics/internal/domain"
	"encoding/json"
	"testing"
	"time"
)

func TestSnapshot_ReflectsObserved(t *testing.T) {
	const route = "/v1/snapshot-probe/{id}"
	latency := requestmetrics.NewRegistry()

	latency.Observe(route, 4*time.Millisecond, 200)

	got, ok := latency.Snapshot().Routes[route]
	if !ok {
		t.Fatalf("route %q missing from snapshot", route)
	}
	if got.Count != 1 {
		t.Errorf("count = %d, want 1", got.Count)
	}
	if len(got.Buckets) == 0 {
		t.Fatal("route latency carries no buckets")
	}
	if last := got.Buckets[len(got.Buckets)-1]; last.LeMs != "+Inf" {
		t.Errorf("final bucket label = %q, want +Inf", last.LeMs)
	}
}

func TestObserve_EmptyRouteRecordsUnmatched(t *testing.T) {
	latency := requestmetrics.NewRegistry()
	latency.Observe("", time.Millisecond, 404)
	if got := latency.Snapshot().Routes["unmatched"].Count; got != 1 {
		t.Fatalf("unmatched count = %d, want 1", got)
	}
}

func TestSnapshot_TalliesStatusClassesPerRoute(t *testing.T) {
	const route = "/v1/status-probe/{id}"
	latency := requestmetrics.NewRegistry()

	latency.Observe(route, time.Millisecond, 200)
	latency.Observe(route, time.Millisecond, 404)
	latency.Observe(route, time.Millisecond, 502)

	got := latency.Snapshot().Routes[route].Status
	if got != (domain.StatusClasses{Count2xx: 1, Count4xx: 1, Count5xx: 1}) {
		t.Errorf("status classes = %+v, want one each of 2xx, 4xx and 5xx", got)
	}
}

func TestNewRegistry_StartsWithOnlyTheUnmatchedAndOverflowRoutesWhateverAnotherRegistrySaw(t *testing.T) {
	used := requestmetrics.NewRegistry()
	used.Observe("/v1/used/{id}", time.Millisecond, 200)

	routes := requestmetrics.NewRegistry().Snapshot().Routes

	unmatched, hasUnmatched := routes["unmatched"]
	overflow, hasOverflow := routes["overflow"]
	if len(routes) != 2 || !hasUnmatched || !hasOverflow || unmatched.Count != 0 || overflow.Count != 0 {
		t.Fatalf("a new registry's routes = %+v, want only unmatched and overflow, both at zero", routes)
	}
}

func TestSnapshot_SerializesTheRoutesAndBucketsOverseerReads(t *testing.T) {
	snap := requestmetrics.Snapshot{Routes: map[string]domain.RouteLatency{
		"/v1/tracks/{trackId}": {Count: 3, SumMs: 12, Buckets: []domain.BucketCount{{LeMs: "5", Count: 2}, {LeMs: "+Inf", Count: 1}}},
	}}

	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var shape struct {
		Routes map[string]struct {
			Count   uint64 `json:"count"`
			SumMs   uint64 `json:"sum_ms"`
			Buckets []struct {
				LeMs  string `json:"le_ms"`
				Count uint64 `json:"count"`
			} `json:"buckets"`
		} `json:"routes"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	route, ok := shape.Routes["/v1/tracks/{trackId}"]
	if !ok {
		t.Fatalf("routes carry no /v1/tracks/{trackId}: %s", raw)
	}
	if route.Count != 3 || route.SumMs != 12 {
		t.Errorf("count, sum_ms = %d, %d, want 3, 12", route.Count, route.SumMs)
	}
	if len(route.Buckets) != 2 || route.Buckets[0].LeMs != "5" || route.Buckets[0].Count != 2 || route.Buckets[1].LeMs != "+Inf" || route.Buckets[1].Count != 1 {
		t.Errorf("buckets = %+v, want le_ms 5 count 2, then le_ms +Inf count 1", route.Buckets)
	}
}

func TestRouteLatency_StatusSerializesUnderStatusKey(t *testing.T) {
	route := domain.RouteLatency{Status: domain.StatusClasses{Count2xx: 7, Count4xx: 3, Count5xx: 1}}

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
