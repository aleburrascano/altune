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

func TestRegistryObserve_CountsSumAndBucket(t *testing.T) {
	reg := newRegistry()
	reg.observe("/r", 3*time.Millisecond)
	reg.observe("/r", 7*time.Millisecond)

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
	reg.observe("/r", -5*time.Millisecond)
	if got := reg.mustLoad("/r").buckets[0]; got != 1 {
		t.Fatalf("negative duration should land in bucket 0, got count %d", got)
	}
}

func TestObserve_EmptyRouteFoldsToUnmatched(t *testing.T) {
	reg := newRegistry()
	reg.observe(unmatchedRoute, time.Millisecond) // Observe folds "" to this key.
	if reg.mustLoad(unmatchedRoute).count != 1 {
		t.Fatal("unmatched route did not record")
	}
}

// TestRegistry_BoundedCardinality proves a flood of distinct routes cannot grow
// the key set without limit: extras fold into the shared overflow key.
func TestRegistry_BoundedCardinality(t *testing.T) {
	reg := newRegistry()
	for i := 0; i < maxRoutes*4; i++ {
		reg.observe("/r/"+strconv.Itoa(i), time.Millisecond)
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
// record into shared and distinct routes at once.
func TestRegistryObserve_Concurrent(t *testing.T) {
	reg := newRegistry()
	const goroutines, perG = 32, 500
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				reg.observe("/shared", time.Millisecond)
				reg.observe("/g/"+strconv.Itoa(g), 2*time.Millisecond)
			}
		}(g)
	}
	wg.Wait()

	if got := reg.mustLoad("/shared").count; got != goroutines*perG {
		t.Fatalf("shared route count = %d, want %d", got, goroutines*perG)
	}
}

func BenchmarkRegistryObserve(b *testing.B) {
	reg := newRegistry()
	reg.observe("/v1/tracks/{trackId}", time.Millisecond) // register before timing
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reg.observe("/v1/tracks/{trackId}", 3*time.Millisecond)
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
