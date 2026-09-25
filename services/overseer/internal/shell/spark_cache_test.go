package shell

import (
	"altune/overseer/internal/core"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSparkCacheReadsTheStoreAtMostOncePerMinute is the load proof: many
// snapshot reads for the same bucket within one refresh window must cost the
// store exactly one read, however many callers (concurrent SSE clients, or a
// tight poll loop) ask for the spark in that window.
func TestSparkCacheReadsTheStoreAtMostOncePerMinute(t *testing.T) {
	var reads atomic.Int64
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := newSparkCache()
	c.now = func() time.Time { return now }
	read := func() ([]core.SparkPoint, error) {
		reads.Add(1)
		return []core.SparkPoint{{At: now, V: 1}}, nil
	}

	const callers = 50
	var wg sync.WaitGroup
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			c.load("reliability", read)
		}()
	}
	wg.Wait()

	// Simulate every 2s SSE tick for a minute straight: still one read, because
	// the clock has not crossed the refresh window.
	for i := 0; i < 29; i++ {
		c.load("reliability", read)
	}

	if got := reads.Load(); got != 1 {
		t.Fatalf("store reads = %d, want 1 for many calls inside one refresh window", got)
	}

	now = now.Add(time.Minute)
	c.load("reliability", read)
	if got := reads.Load(); got != 2 {
		t.Fatalf("store reads = %d, want 2 after the refresh window elapses", got)
	}
}

// TestSparkCacheServesLastGoodSparkOnAFailedRead proves a failing refresh keeps
// serving the last successful spark rather than dropping it, and that a failure
// streak logs once, not once per call (noteFailure only flips failing on the
// first failure of a streak).
func TestSparkCacheServesLastGoodSparkOnAFailedRead(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := newSparkCache()
	c.now = func() time.Time { return now }
	good := []core.SparkPoint{{At: now, V: 42}}
	c.load("cost", func() ([]core.SparkPoint, error) { return good, nil })

	now = now.Add(time.Minute)
	var failReads int
	failing := func() ([]core.SparkPoint, error) {
		failReads++
		return nil, errBoom
	}
	got := c.load("cost", failing)
	if len(got) != 1 || got[0].V != 42 {
		t.Fatalf("spark = %+v, want the last good spark preserved on a failed read", got)
	}

	now = now.Add(time.Minute)
	_ = c.load("cost", failing)
	if failReads != 2 {
		t.Fatalf("failing read ran %d times across two refresh windows, want 2 (one per window)", failReads)
	}
}

type recordingTailReader struct {
	calls int
	limit int
}

func (r *recordingTailReader) Names(string) ([]string, error) { return nil, nil }

func (r *recordingTailReader) Query(string, string, time.Time, time.Time) ([]core.Point, error) {
	r.calls++
	return []core.Point{{At: time.Now(), Value: -1}}, nil
}

func (r *recordingTailReader) Tail(_, _ string, _, _ time.Time, limit int) ([]core.Point, error) {
	r.limit = limit
	return []core.Point{{At: time.Now(), Value: 1}}, nil
}

// TestQueryTailPrefersTheTailReaderPushDown proves the spark read pushes the
// LIMIT into SQL via TailReader when the reader supports it, rather than
// falling back to a full Query and trimming in Go.
func TestQueryTailPrefersTheTailReaderPushDown(t *testing.T) {
	reader := &recordingTailReader{}
	h := NewHandler(fixedRegistryStub{}, WithSeries(reader))

	points, err := h.queryTail("reliability", "latency_ms", time.Now().Add(-time.Hour), time.Now())
	if err != nil {
		t.Fatalf("queryTail: %v", err)
	}
	if reader.calls != 0 {
		t.Fatalf("Query called %d times, want 0: TailReader should be preferred", reader.calls)
	}
	if reader.limit != sparkPoints {
		t.Fatalf("Tail limit = %d, want %d", reader.limit, sparkPoints)
	}
	if len(points) != 1 || points[0].Value != 1 {
		t.Fatalf("points = %+v, want the Tail result", points)
	}
}

type fixedRegistryStub struct{}

func (fixedRegistryStub) Buckets() []core.Bucket         { return nil }
func (fixedRegistryStub) Get(string) (core.Bucket, bool) { return nil, false }

type sparkCacheError struct{ msg string }

func (e sparkCacheError) Error() string { return e.msg }

var errBoom = sparkCacheError{"store unreachable"}
