package backendperf_test

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"testing"
)

func searchSnapshot(count, ok, serverErrors uint64, within map[string]uint64) goapi.LiveMetrics {
	return liveWith(map[string]goapi.RouteLatency{
		"/v1/discovery/search": {
			Count:   count,
			Buckets: hist(within),
			Status:  goapi.StatusClasses{Count2xx: ok, Count5xx: serverErrors},
		},
	})
}

func sloNamed(t *testing.T, statuses []core.SLOStatus, name string) core.SLOStatus {
	t.Helper()
	for _, s := range statuses {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no SLO named %q in %+v", name, statuses)
	return core.SLOStatus{}
}

func TestSearchSLOsCountOnlyTrafficSeenBetweenTwoPolls(t *testing.T) {
	reader := &fakeReader{}
	reader.set(searchSnapshot(1000, 1000, 0, map[string]uint64{"100": 1000}), nil)
	b := newBucket(reader)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("first collect: %v", err)
	}

	first := snapData(t, b.Snapshot()).SLOs

	for _, s := range first {
		if s.Measured {
			t.Fatalf("SLO %q measured after the first poll, which only sets the baseline: %+v", s.Name, s)
		}
	}

	reader.set(searchSnapshot(1100, 1090, 10, map[string]uint64{"100": 1000, "5000": 100}), nil)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("second collect: %v", err)
	}
	second := snapData(t, b.Snapshot()).SLOs

	availability := sloNamed(t, second, "search answers without a server error")
	if availability.Total != 100 || availability.Good != 90 || availability.Met {
		t.Fatalf("availability = %+v, want 90 good of the 100 new requests and not met", availability)
	}
	latency := sloNamed(t, second, "search answers within 1 s")
	if latency.Total != 100 || latency.Good != 0 || latency.Met {
		t.Fatalf("latency = %+v, want none of the 100 new requests within 1 s", latency)
	}
}

func TestSearchSLOsMeetTheirTargetsOnFastCleanTraffic(t *testing.T) {
	reader := &fakeReader{}
	reader.set(searchSnapshot(0, 0, 0, nil), nil)
	b := newBucket(reader)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("first collect: %v", err)
	}
	reader.set(searchSnapshot(500, 500, 0, map[string]uint64{"100": 500}), nil)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("second collect: %v", err)
	}

	for _, s := range snapData(t, b.Snapshot()).SLOs {
		if !s.Measured || !s.Met || s.BudgetRemaining != 1 {
			t.Fatalf("SLO %q = %+v, want measured, met and with the whole budget left", s.Name, s)
		}
	}
}

func TestSearchSLOsIgnoreOtherRoutes(t *testing.T) {
	reader := &fakeReader{}
	other := func(count uint64) goapi.LiveMetrics {
		return liveWith(map[string]goapi.RouteLatency{
			"/v1/tracks/{trackId}": {Count: count, Buckets: hist(map[string]uint64{"5000": count}), Status: goapi.StatusClasses{Count5xx: count}},
		})
	}
	reader.set(other(10), nil)
	b := newBucket(reader)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("first collect: %v", err)
	}
	reader.set(other(500), nil)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("second collect: %v", err)
	}

	for _, s := range snapData(t, b.Snapshot()).SLOs {
		if s.Measured {
			t.Fatalf("SLO %q counted another route: %+v", s.Name, s)
		}
	}
}
