package reqmetrics

import (
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestBucketIndex(t *testing.T) {
	cases := []struct {
		ms   int64
		want int
	}{
		{0, 0},
		{1, 0},
		{2, 1},
		{5, 1},
		{6, 2},
		{5000, numBuckets - 2},
		{5001, numBuckets - 1},
		{1 << 30, numBuckets - 1},
	}
	for _, c := range cases {
		if got := bucketIndex(c.ms); got != c.want {
			t.Errorf("bucketIndex(%d) = %d, want %d", c.ms, got, c.want)
		}
	}
}

func TestStatusClassIndex(t *testing.T) {
	cases := []struct {
		status int
		want   int
	}{
		{200, class2xx},
		{204, class2xx},
		{404, class4xx},
		{429, class4xx},
		{500, class5xx},
		{503, class5xx},
		{100, -1},
		{301, -1},
		{0, -1},
		{-5, -1},
		{600, -1},
		{999, -1},
	}
	for _, c := range cases {
		if got := statusClassIndex(c.status); got != c.want {
			t.Errorf("statusClassIndex(%d) = %d, want %d", c.status, got, c.want)
		}
	}
}

func TestRegistryObserve_TalliesStatusClass(t *testing.T) {
	reg := newRegistry()
	reg.observe("/r", time.Millisecond, 200)
	reg.observe("/r", time.Millisecond, 204)
	reg.observe("/r", time.Millisecond, 404)
	reg.observe("/r", time.Millisecond, 500)
	reg.observe("/r", time.Millisecond, 302) // 3xx: latency only, no status class

	h := reg.mustLoad("/r")
	if got := h.statusClass[class2xx]; got != 2 {
		t.Errorf("2xx = %d, want 2", got)
	}
	if got := h.statusClass[class4xx]; got != 1 {
		t.Errorf("4xx = %d, want 1", got)
	}
	if got := h.statusClass[class5xx]; got != 1 {
		t.Errorf("5xx = %d, want 1", got)
	}
	if got := h.count; got != 5 {
		t.Errorf("count = %d, want 5 — every request counts toward latency", got)
	}
}

func TestRegistryObserve_CountsSumAndBucket(t *testing.T) {
	reg := newRegistry()
	reg.observe("/r", 3*time.Millisecond, 200)
	reg.observe("/r", 7*time.Millisecond, 200)

	h := reg.mustLoad("/r")
	if got := h.count; got != 2 {
		t.Fatalf("count = %d, want 2", got)
	}
	if got := h.sumMs; got != 10 {
		t.Fatalf("sumMs = %d, want 10", got)
	}
	if got := h.buckets[bucketIndex(3)]; got != 1 {
		t.Errorf("bucket for 3ms = %d, want 1", got)
	}
	if got := h.buckets[bucketIndex(7)]; got != 1 {
		t.Errorf("bucket for 7ms = %d, want 1", got)
	}
}

func TestRegistryObserve_NegativeDurationClampsToZero(t *testing.T) {
	reg := newRegistry()
	reg.observe("/r", -5*time.Millisecond, 200)
	if got := reg.mustLoad("/r").buckets[0]; got != 1 {
		t.Fatalf("negative duration should land in bucket 0, got count %d", got)
	}
}

func TestObserve_EmptyRouteFoldsToUnmatched(t *testing.T) {
	reg := newRegistry()
	reg.observe(unmatchedRoute, time.Millisecond, 404) // Observe folds "" to this key.
	if reg.mustLoad(unmatchedRoute).count != 1 {
		t.Fatal("unmatched route did not record")
	}
}

// TestRegistry_BoundedCardinality proves a flood of distinct routes cannot grow
// the key set without limit: extras fold into the shared overflow key.
func TestRegistry_BoundedCardinality(t *testing.T) {
	reg := newRegistry()
	for i := 0; i < maxRoutes*4; i++ {
		reg.observe("/r/"+strconv.Itoa(i), time.Millisecond, 200)
	}
	got := 0
	reg.routes.Range(func(_, _ any) bool { got++; return true })

	// maxRoutes dynamic keys + the two reserved keys (unmatched, overflow).
	if want := maxRoutes + 2; got > want {
		t.Fatalf("distinct route keys = %d, want <= %d (overflow must fold)", got, want)
	}
	if reg.mustLoad(overflowRoute).count == 0 {
		t.Fatal("routes past the cap should have folded into the overflow key")
	}
}

// TestRegistryObserve_Concurrent exercises the -race detector: many goroutines
// record into shared and distinct routes at once, each cycling through the
// status classes so the status counters share the same race scrutiny.
func TestRegistryObserve_Concurrent(t *testing.T) {
	reg := newRegistry()
	const goroutines, perG = 32, 500
	statuses := [...]int{200, 404, 500}
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				reg.observe("/shared", time.Millisecond, statuses[i%len(statuses)])
				reg.observe("/g/"+strconv.Itoa(g), 2*time.Millisecond, 200)
			}
		}(g)
	}
	wg.Wait()

	shared := reg.mustLoad("/shared")
	if got := shared.count; got != goroutines*perG {
		t.Fatalf("shared route count = %d, want %d", got, goroutines*perG)
	}
	total := shared.statusClass[class2xx] + shared.statusClass[class4xx] + shared.statusClass[class5xx]
	if total != uint64(goroutines*perG) {
		t.Fatalf("shared status tally = %d, want %d — no update was lost to a race", total, goroutines*perG)
	}
}

func BenchmarkRegistryObserve(b *testing.B) {
	reg := newRegistry()
	reg.observe("/v1/tracks/{trackId}", time.Millisecond, 200) // register before timing
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reg.observe("/v1/tracks/{trackId}", 3*time.Millisecond, 200)
	}
}

// TestRegistryObserve_ZeroAllocHotPath is the Plant invariant: recording into an
// already-seen route allocates nothing on the hot path.
func TestRegistryObserve_ZeroAllocHotPath(t *testing.T) {
	res := testing.Benchmark(BenchmarkRegistryObserve)
	if got := res.AllocsPerOp(); got != 0 {
		t.Fatalf("observe hot path = %d allocs/op, want 0", got)
	}
}
