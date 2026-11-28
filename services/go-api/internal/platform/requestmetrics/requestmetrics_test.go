package requestmetrics_test

import (
	"altune/go-api/internal/platform/requestmetrics"
	"altune/go-api/internal/platform/requestmetrics/internal/service"
	"testing"
	"time"
)

func TestSnapshot_ReflectsObserved(t *testing.T) {
	const route = "/v1/snapshot-probe/{id}"
	latency := requestmetrics.New()

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
	latency := requestmetrics.New()
	latency.Observe("", time.Millisecond, 404)
	if got := latency.Snapshot().Routes["unmatched"].Count; got != 1 {
		t.Fatalf("unmatched count = %d, want 1", got)
	}
}

func TestSnapshot_TalliesStatusClassesPerRoute(t *testing.T) {
	const route = "/v1/status-probe/{id}"
	latency := requestmetrics.New()

	latency.Observe(route, time.Millisecond, 200)
	latency.Observe(route, time.Millisecond, 404)
	latency.Observe(route, time.Millisecond, 502)

	got := latency.Snapshot().Routes[route].Status
	if got != (service.StatusClasses{Count2xx: 1, Count4xx: 1, Count5xx: 1}) {
		t.Errorf("status classes = %+v, want one each of 2xx, 4xx and 5xx", got)
	}
}

func TestNewRegistry_StartsWithOnlyTheUnmatchedAndOverflowRoutesWhateverAnotherRegistrySaw(t *testing.T) {
	used := requestmetrics.New()
	used.Observe("/v1/used/{id}", time.Millisecond, 200)

	routes := requestmetrics.New().Snapshot().Routes

	unmatched, hasUnmatched := routes["unmatched"]
	overflow, hasOverflow := routes["overflow"]
	if len(routes) != 2 || !hasUnmatched || !hasOverflow || unmatched.Count != 0 || overflow.Count != 0 {
		t.Fatalf("a new registry's routes = %+v, want only unmatched and overflow, both at zero", routes)
	}
}
