package backendperf

import (
	"altune/overseer/internal/goapi"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// fakeReader is a controllable stand-in for the live-metrics read path. A test
// sets the snapshot and/or error it returns, exercising collect, render and the
// degrade-to-stale path with no network.
type fakeReader struct {
	mu   sync.Mutex
	live goapi.LiveMetrics
	err  error
}

func (f *fakeReader) AdminMetricsLive(context.Context) (goapi.LiveMetrics, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.live, f.err
}

func (f *fakeReader) set(live goapi.LiveMetrics, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.live, f.err = live, err
}

func srcDown() error {
	return &goapi.SourceDownError{Op: "GET /admin/metrics/live", Err: errors.New("dial refused")}
}

func liveWith(routes map[string]goapi.RouteLatency) goapi.LiveMetrics {
	return goapi.LiveMetrics{Latency: goapi.LatencyMetrics{Routes: routes}}
}

// collectStore runs one collect/store cycle, the pair the shell drives on a tick.
func collectStore(t *testing.T, b *Bucket) error {
	t.Helper()
	signals, err := b.Collect(context.Background())
	b.Store(signals)
	return err
}

// TestCollectStoreRender is the core Done proof: a live histogram flows into the
// bucket and renders per-route percentiles plus the throughput trend.
func TestCollectStoreRender(t *testing.T) {
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/v1/tracks/{trackId}": {Count: 100, SumMs: 750, Buckets: hist(map[string]uint64{"10": 100})},
	}), nil)
	b := newBucket(reader)

	if body := string(b.Render().Body); !strings.Contains(body, "no route latency yet") {
		t.Errorf("empty render = %q, want a no-latency gap", body)
	}

	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect while live = %v, want nil", err)
	}
	body := string(b.Render().Body)
	if !strings.Contains(body, "LIVE") {
		t.Errorf("render missing LIVE marker:\n%s", body)
	}
	if !strings.Contains(body, "/v1/tracks/{trackId}") {
		t.Errorf("render missing the route:\n%s", body)
	}
	if !strings.Contains(body, "p99") || !strings.Contains(body, "ms") {
		t.Errorf("render missing percentiles:\n%s", body)
	}
	if !strings.Contains(body, "Throughput trend") {
		t.Errorf("render missing throughput trend:\n%s", body)
	}
}

// TestSlowestRouteHighlighted proves the slowest route by p99 heads the highlight
// list and carries the "slow" emphasis class.
func TestSlowestRouteHighlighted(t *testing.T) {
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/fast": {Count: 100, Buckets: hist(map[string]uint64{"10": 100})},
		"/slow": {Count: 100, Buckets: hist(map[string]uint64{"1000": 100})},
	}), nil)
	b := newBucket(reader)

	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	body := string(b.Render().Body)

	slowIdx := strings.Index(body, "/slow")
	fastIdx := strings.Index(body, "/fast")
	if slowIdx < 0 || fastIdx < 0 || slowIdx > fastIdx {
		t.Errorf("slowest route not first: slowIdx=%d fastIdx=%d\n%s", slowIdx, fastIdx, body)
	}
	if !strings.Contains(body, `<li class="slow">/slow`) {
		t.Errorf("slowest route not emphasised with the slow class:\n%s", body)
	}
}

// TestDegradesToStaleOnSourceDown proves degrade-don't-crash: after a live read,
// an unreachable read flags the panel STALE while still showing the last-known
// latency, and the collect error surfaces for the shell to keep last-known state.
func TestDegradesToStaleOnSourceDown(t *testing.T) {
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/v1/tracks/{trackId}": {Count: 100, Buckets: hist(map[string]uint64{"10": 100})},
	}), nil)
	b := newBucket(reader)

	if err := collectStore(t, b); err != nil {
		t.Fatalf("initial Collect: %v", err)
	}
	if body := string(b.Render().Body); strings.Contains(body, "STALE") {
		t.Fatalf("pre-degrade render unexpectedly STALE:\n%s", body)
	}

	reader.set(goapi.LiveMetrics{}, srcDown())
	err := collectStore(t, b)
	if err == nil {
		t.Fatal("Collect with source down returned nil error")
	}
	// The wrapped error must still classify as source-down for the shell.
	if !goapi.IsSourceDown(err) {
		t.Errorf("degraded collect error is not source-down: %v", err)
	}
	body := string(b.Render().Body)
	if !strings.Contains(body, "STALE") {
		t.Errorf("post-degrade render = %q, want a STALE flag", body)
	}
	if !strings.Contains(body, "/v1/tracks/{trackId}") {
		t.Errorf("stale render dropped the last-known route:\n%s", body)
	}
}

// TestRenderEscapesWatchedAppRoute proves route templates — watched-app text from
// go-api — are HTML-escaped, so a hostile route name cannot inject markup into the
// trusted panel HTML.
func TestRenderEscapesWatchedAppRoute(t *testing.T) {
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		`/x/<script>alert(1)</script>`: {Count: 10, Buckets: hist(map[string]uint64{"10": 10})},
	}), nil)
	b := newBucket(reader)

	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	body := string(b.Render().Body)
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Errorf("route template rendered unescaped:\n%s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("route template not HTML-escaped:\n%s", body)
	}
}

// TestStaysBoundedUnderLoad proves the throughput history is bounded: far more
// collect cycles than the ring capacity never grow the store past its cap.
func TestStaysBoundedUnderLoad(t *testing.T) {
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/a": {Count: 1, Buckets: hist(map[string]uint64{"10": 1})},
	}), nil)
	b := newBucket(reader)

	for i := 0; i < historyCapacity*3; i++ {
		if err := collectStore(t, b); err != nil {
			t.Fatalf("Collect #%d: %v", i, err)
		}
	}
	if got := b.history.Len(); got > historyCapacity {
		t.Errorf("history Len = %d, exceeds cap %d", got, historyCapacity)
	}
	if b.history.Cap() != historyCapacity {
		t.Errorf("history Cap = %d, want %d", b.history.Cap(), historyCapacity)
	}
}

// TestConcurrentCollectAndRender proves the collect loop and the HTTP render can
// run at once without a data race (asserted under -race): render copies the
// snapshot under the lock and reads it after.
func TestConcurrentCollectAndRender(t *testing.T) {
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/a": {Count: 5, Buckets: hist(map[string]uint64{"10": 5})},
	}), nil)
	b := newBucket(reader)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			_ = collectStore(t, b)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			_ = b.Render()
		}
	}()
	wg.Wait()
}

// TestUnconfiguredDegradesNotCrashes proves an unconfigured bucket (null reader)
// never panics: collect reports source-down and render serves a stale/empty panel.
func TestUnconfiguredDegradesNotCrashes(t *testing.T) {
	b := newBucket(nullReader{})

	if _, err := b.Collect(context.Background()); !goapi.IsSourceDown(err) {
		t.Fatalf("unconfigured Collect error = %v, want source-down", err)
	}
	body := string(b.Render().Body)
	if !strings.Contains(body, "STALE") {
		t.Errorf("unconfigured render = %q, want STALE", body)
	}
}
