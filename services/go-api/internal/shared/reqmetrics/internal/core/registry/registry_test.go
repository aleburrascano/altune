package registry_test

import (
	"altune/go-api/internal/shared/reqmetrics/internal/core/registry"
	"strconv"
	"sync"
	"testing"
	"time"
)

var bucketLabels = []string{"1", "5", "10", "25", "50", "100", "250", "500", "1000", "2500", "5000", "+Inf"}

func observedOnce(t *testing.T, d time.Duration, status int) registry.RouteLatency {
	t.Helper()
	reg := registry.New()
	reg.Observe("/r", d, status)
	got, ok := reg.Snapshot().Routes["/r"]
	if !ok {
		t.Fatalf("route /r missing after Observe(%v, %d)", d, status)
	}
	return got
}

func filledBucket(t *testing.T, got registry.RouteLatency) string {
	t.Helper()
	filled := ""
	for _, b := range got.Buckets {
		switch {
		case b.Count == 0:
		case b.Count == 1 && filled == "":
			filled = b.LeMs
		default:
			t.Fatalf("buckets = %+v, want exactly one bucket holding one observation", got.Buckets)
		}
	}
	return filled
}

func TestRegistry_SnapshotListsEveryBucketInOrder(t *testing.T) {
	got := observedOnce(t, time.Millisecond, 200)

	if len(got.Buckets) != len(bucketLabels) {
		t.Fatalf("buckets = %+v, want %d buckets", got.Buckets, len(bucketLabels))
	}
	for i, want := range bucketLabels {
		if got.Buckets[i].LeMs != want {
			t.Errorf("bucket %d le_ms = %q, want %q", i, got.Buckets[i].LeMs, want)
		}
	}
}

func TestRegistry_ObservationLandsInTheFirstBucketItFits(t *testing.T) {
	cases := map[time.Duration]string{
		0:                            "1",
		1 * time.Millisecond:         "1",
		2 * time.Millisecond:         "5",
		5 * time.Millisecond:         "5",
		6 * time.Millisecond:         "10",
		5000 * time.Millisecond:      "5000",
		5001 * time.Millisecond:      "+Inf",
		(1 << 30) * time.Millisecond: "+Inf",
	}
	for d, want := range cases {
		if got := filledBucket(t, observedOnce(t, d, 200)); got != want {
			t.Errorf("Observe(%v) filled bucket %q, want %q", d, got, want)
		}
	}
}

func TestRegistry_EveryBoundIsInclusive(t *testing.T) {
	for i, label := range bucketLabels[:len(bucketLabels)-1] {
		bound, err := strconv.Atoi(label)
		if err != nil {
			t.Fatal(err)
		}
		at := time.Duration(bound) * time.Millisecond
		if got := filledBucket(t, observedOnce(t, at, 200)); got != label {
			t.Errorf("Observe(%v) filled bucket %q, want %q", at, got, label)
		}
		past := at + time.Millisecond
		if got := filledBucket(t, observedOnce(t, past, 200)); got != bucketLabels[i+1] {
			t.Errorf("Observe(%v) filled bucket %q, want %q", past, got, bucketLabels[i+1])
		}
	}
}

func TestRegistry_TalliesStatusClasses(t *testing.T) {
	cases := map[int]registry.StatusClasses{
		200: {Count2xx: 1},
		204: {Count2xx: 1},
		299: {Count2xx: 1},
		404: {Count4xx: 1},
		429: {Count4xx: 1},
		500: {Count5xx: 1},
		503: {Count5xx: 1},
		100: {},
		301: {},
		0:   {},
		-5:  {},
		600: {},
		999: {},
	}
	for status, want := range cases {
		got := observedOnce(t, time.Millisecond, status)
		if got.Status != want {
			t.Errorf("status %d tallied %+v, want %+v", status, got.Status, want)
		}
		if got.Count != 1 {
			t.Errorf("status %d: count = %d, want 1, every request counts toward latency", status, got.Count)
		}
	}
}

func TestRegistry_CountsSumAndBuckets(t *testing.T) {
	reg := registry.New()
	reg.Observe("/r", 3*time.Millisecond, 200)
	reg.Observe("/r", 7*time.Millisecond, 200)

	got := reg.Snapshot().Routes["/r"]
	if got.Count != 2 || got.SumMs != 10 {
		t.Fatalf("count, sum_ms = %d, %d, want 2, 10", got.Count, got.SumMs)
	}
	if got.Buckets[1].Count != 1 || got.Buckets[2].Count != 1 {
		t.Errorf("buckets = %+v, want one in le 5 and one in le 10", got.Buckets)
	}
}

func TestRegistry_NegativeDurationCountsAsZero(t *testing.T) {
	got := observedOnce(t, -5*time.Millisecond, 200)

	if got.SumMs != 0 || got.Buckets[0].Count != 1 {
		t.Fatalf("sum_ms = %d, buckets = %+v, want 0 and the first bucket", got.SumMs, got.Buckets)
	}
}

func TestRegistry_EmptyRouteFoldsToUnmatched(t *testing.T) {
	reg := registry.New()
	reg.Observe("", time.Millisecond, 404)

	snap := reg.Snapshot()
	if snap.Routes["unmatched"].Count != 1 {
		t.Fatalf("unmatched count = %d, want 1", snap.Routes["unmatched"].Count)
	}
	if _, ok := snap.Routes[""]; ok {
		t.Fatal("an empty route got its own key")
	}
}

func TestRegistry_FreshSnapshotHoldsOnlyTheEmptyReservedRoutes(t *testing.T) {
	snap := registry.New().Snapshot()

	if len(snap.Routes) != 2 || snap.Routes["unmatched"].Count != 0 || snap.Routes["overflow"].Count != 0 {
		t.Fatalf("fresh snapshot = %+v, want only empty unmatched and overflow", snap.Routes)
	}
	if _, ok := snap.Routes["overflow"]; !ok {
		t.Fatal("fresh snapshot lacks the overflow route")
	}
}

func TestRegistry_FoldsRoutesPastTheCapIntoOverflow(t *testing.T) {
	const maxRoutes, observed = 256, 1024
	reg := registry.New()
	for i := range observed {
		reg.Observe("/r/"+strconv.Itoa(i), time.Millisecond, 200)
	}

	snap := reg.Snapshot()
	if got, want := len(snap.Routes), maxRoutes+2; got != want {
		t.Fatalf("distinct route keys = %d, want %d (the cap plus unmatched and overflow)", got, want)
	}
	if got := snap.Routes["overflow"].Count; got != observed-maxRoutes {
		t.Fatalf("overflow count = %d, want %d", got, observed-maxRoutes)
	}
	reg.Observe("/r/0", time.Millisecond, 200)
	if got := reg.Snapshot().Routes["/r/0"].Count; got != 2 {
		t.Fatalf("a route admitted before the cap counted %d, want 2: it keeps its own key", got)
	}
}

func TestRegistry_ConcurrentObservationsLoseNoUpdate(t *testing.T) {
	reg := registry.New()
	const goroutines, perG = 32, 500
	statuses := [...]int{200, 404, 500}
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range perG {
				reg.Observe("/shared", time.Millisecond, statuses[i%len(statuses)])
				reg.Observe("/g/"+strconv.Itoa(g), 2*time.Millisecond, 200)
			}
		}()
	}
	wg.Wait()

	shared := reg.Snapshot().Routes["/shared"]
	if shared.Count != goroutines*perG {
		t.Fatalf("shared route count = %d, want %d", shared.Count, goroutines*perG)
	}
	if total := shared.Status.Count2xx + shared.Status.Count4xx + shared.Status.Count5xx; total != goroutines*perG {
		t.Fatalf("shared status tally = %d, want %d, no update was lost to a race", total, goroutines*perG)
	}
}

func TestRegistry_ObserveOnAKnownRouteAllocatesNothing(t *testing.T) {
	reg := registry.New()
	reg.Observe("/v1/tracks/{trackId}", time.Millisecond, 200)

	allocs := testing.AllocsPerRun(1000, func() {
		reg.Observe("/v1/tracks/{trackId}", 3*time.Millisecond, 200)
	})

	if allocs != 0 {
		t.Fatalf("Observe hot path = %v allocs/op, want 0", allocs)
	}
}

func BenchmarkRegistryObserve(b *testing.B) {
	reg := registry.New()
	reg.Observe("/v1/tracks/{trackId}", time.Millisecond, 200)
	b.ReportAllocs()
	for b.Loop() {
		reg.Observe("/v1/tracks/{trackId}", 3*time.Millisecond, 200)
	}
}
