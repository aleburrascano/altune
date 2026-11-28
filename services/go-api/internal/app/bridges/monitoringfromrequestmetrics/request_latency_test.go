package monitoringfromrequestmetrics_test

import (
	"altune/go-api/internal/app/bridges/monitoringfromrequestmetrics"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/platform/requestmetrics"
	"testing"
	"time"
)

func TestRequestLatency_CarriesEveryCountBucketAndStatusClassOfARoute(t *testing.T) {
	registry := requestmetrics.New()
	registry.Observe("/v1/tracks/{id}", 5*time.Millisecond, 200)
	registry.Observe("/v1/tracks/{id}", 7*time.Millisecond, 404)
	registry.Observe("/v1/tracks/{id}", 9*time.Millisecond, 503)

	got := monitoringfromrequestmetrics.NewRequestLatency(registry).RequestLatency()

	want := registry.Snapshot().Routes["/v1/tracks/{id}"]
	route, ok := got.Routes["/v1/tracks/{id}"]
	if !ok {
		t.Fatalf("route missing from %+v", got)
	}
	if route.Count != want.Count || route.SumMs != want.SumMs {
		t.Errorf("count/sum = %d/%d, want %d/%d", route.Count, route.SumMs, want.Count, want.SumMs)
	}
	if route.Status != (monitoring.StatusClasses{Count2xx: want.Status.Count2xx, Count4xx: want.Status.Count4xx, Count5xx: want.Status.Count5xx}) {
		t.Errorf("status = %+v, want %+v", route.Status, want.Status)
	}
	if len(route.Buckets) != len(want.Buckets) || len(route.Buckets) == 0 {
		t.Fatalf("buckets = %d, want %d", len(route.Buckets), len(want.Buckets))
	}
	for i, b := range route.Buckets {
		if b.LeMs != want.Buckets[i].LeMs || b.Count != want.Buckets[i].Count {
			t.Errorf("bucket %d = %+v, want %+v", i, b, want.Buckets[i])
		}
	}
}

func TestRequestLatency_AnEmptyRegistryObservedNoRequests(t *testing.T) {
	got := monitoringfromrequestmetrics.NewRequestLatency(requestmetrics.New()).RequestLatency()

	for route, latency := range got.Routes {
		if latency.Count != 0 {
			t.Errorf("route %q count = %d, want 0", route, latency.Count)
		}
	}
}
