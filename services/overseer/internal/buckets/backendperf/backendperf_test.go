package backendperf

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

// fakeReader is a controllable stand-in for the live-metrics read path. A test
// sets the snapshot and/or error it returns, exercising collect, snapshot and the
// degrade-to-source-down path with no network.
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

func collectStore(t *testing.T, b *Bucket) error {
	t.Helper()
	signals, err := b.Collect(context.Background())
	b.Store(signals)
	return err
}

func snapData(t *testing.T, snap core.Snapshot) Data {
	t.Helper()
	var d Data
	if err := json.Unmarshal(snap.Data, &d); err != nil {
		t.Fatalf("unmarshal data: %v (%s)", err, snap.Data)
	}
	return d
}

// TestCollectStoreSnapshot is the core Done proof: a live histogram flows into the
// bucket and the snapshot carries per-route percentiles plus the throughput trend.
func TestCollectStoreSnapshot(t *testing.T) {
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/v1/tracks/{trackId}": {Count: 100, SumMs: 750, Buckets: hist(map[string]uint64{"10": 100})},
	}), nil)
	b := newBucket(reader)

	empty := b.Snapshot()
	if empty.State != core.StateStale {
		t.Errorf("empty state = %q, want stale (no data yet)", empty.State)
	}
	if got := len(snapData(t, empty).Routes); got != 0 {
		t.Errorf("empty routes = %d, want 0", got)
	}

	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect while live = %v, want nil", err)
	}
	snap := b.Snapshot()
	if snap.State != core.StateLive {
		t.Errorf("state = %q, want live", snap.State)
	}
	d := snapData(t, snap)
	if len(d.Routes) != 1 || d.Routes[0].Route != "/v1/tracks/{trackId}" {
		t.Fatalf("routes = %+v, want the one route", d.Routes)
	}
	if d.Routes[0].P99.Ms <= 0 {
		t.Errorf("p99 = %v, want a positive estimate", d.Routes[0].P99)
	}
	if len(d.Throughput) == 0 {
		t.Errorf("throughput trend empty, want a sample")
	}
}

// TestSlowestRouteFirst proves the slowest route by p99 heads the list.
func TestSlowestRouteFirst(t *testing.T) {
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/fast": {Count: 100, Buckets: hist(map[string]uint64{"10": 100})},
		"/slow": {Count: 100, Buckets: hist(map[string]uint64{"1000": 100})},
	}), nil)
	b := newBucket(reader)

	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	d := snapData(t, b.Snapshot())
	if len(d.Routes) < 2 || d.Routes[0].Route != "/slow" {
		t.Errorf("slowest route not first: %+v", d.Routes)
	}
}

// TestDegradesToSourceDown proves degrade-don't-crash: after a live read, an
// unreachable read flips the panel source_down while still carrying the last-known
// latency, and the collect error surfaces as source-down for the shell.
func TestDegradesToSourceDown(t *testing.T) {
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/v1/tracks/{trackId}": {Count: 100, Buckets: hist(map[string]uint64{"10": 100})},
	}), nil)
	b := newBucket(reader)

	if err := collectStore(t, b); err != nil {
		t.Fatalf("initial Collect: %v", err)
	}
	if snap := b.Snapshot(); snap.State != core.StateLive {
		t.Fatalf("pre-degrade state = %q, want live", snap.State)
	}

	reader.set(goapi.LiveMetrics{}, srcDown())
	err := collectStore(t, b)
	if err == nil {
		t.Fatal("Collect with source down returned nil error")
	}
	if !goapi.IsSourceDown(err) {
		t.Errorf("degraded collect error is not source-down: %v", err)
	}
	snap := b.Snapshot()
	if snap.State != core.StateSourceDown {
		t.Errorf("post-degrade state = %q, want source_down", snap.State)
	}
	if d := snapData(t, snap); len(d.Routes) != 1 || d.Routes[0].Route != "/v1/tracks/{trackId}" {
		t.Errorf("stale snapshot dropped the last-known route: %+v", d.Routes)
	}
}

// TestSnapshotCarriesRawRoute proves route templates — watched-app text from
// go-api — are carried VERBATIM in the JSON payload (React escapes them on render,
// the escaping invariant moved off html/template onto the client).
func TestSnapshotCarriesRawRoute(t *testing.T) {
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		`/x/<script>alert(1)</script>`: {Count: 10, Buckets: hist(map[string]uint64{"10": 10})},
	}), nil)
	b := newBucket(reader)

	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	d := snapData(t, b.Snapshot())
	if len(d.Routes) != 1 || !strings.Contains(d.Routes[0].Route, "<script>alert(1)</script>") {
		t.Errorf("route not carried verbatim for the frontend to escape: %+v", d.Routes)
	}
}

// TestStaysBoundedUnderLoad proves the throughput history is bounded.
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

// TestConcurrentCollectAndSnapshot proves the collect loop and the HTTP read can
// run at once without a data race (asserted under -race).
func TestConcurrentCollectAndSnapshot(t *testing.T) {
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
			_ = b.Snapshot()
		}
	}()
	wg.Wait()
}

// TestUnconfiguredDegradesNotCrashes proves an unconfigured bucket (null reader)
// never panics: collect reports source-down and the snapshot is source_down.
func TestUnconfiguredDegradesNotCrashes(t *testing.T) {
	b := newBucket(nullReader{})

	if _, err := b.Collect(context.Background()); !goapi.IsSourceDown(err) {
		t.Fatalf("unconfigured Collect error = %v, want source-down", err)
	}
	if snap := b.Snapshot(); snap.State != core.StateSourceDown {
		t.Errorf("unconfigured state = %q, want source_down", snap.State)
	}
}
