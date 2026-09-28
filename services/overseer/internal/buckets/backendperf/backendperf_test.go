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
	return &goapi.SourceDownError{Op: "GET /observe/metrics/live", Err: errors.New("dial refused")}
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

func TestSeverityCriticalWhenSlowestRouteIsHot(t *testing.T) {
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/v1/discovery/search": {Count: 100, Buckets: hist(map[string]uint64{"1000": 100})},
	}), nil)
	b := newBucket(reader)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	snap := b.Snapshot()

	if snap.Severity != core.SeverityCritical {
		t.Errorf("severity = %q, want critical", snap.Severity)
	}
	if snap.State != core.StateLive {
		t.Errorf("state = %q, want live — a slow route is not a stale source", snap.State)
	}
	if snap.Headline != "slowest p99 995 ms — /v1/discovery/search" {
		t.Errorf("headline = %q, want the worst p99 and its route", snap.Headline)
	}
}

func TestSeverityWarnsInTheAmberBand(t *testing.T) {
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/v1/library": {Count: 100, Buckets: hist(map[string]uint64{"250": 100})},
	}), nil)
	b := newBucket(reader)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	snap := b.Snapshot()

	if snap.Severity != core.SeverityWarn {
		t.Errorf("severity = %q, want warn", snap.Severity)
	}
	if !strings.Contains(snap.Headline, "/v1/library") {
		t.Errorf("headline = %q, want the slowest route named", snap.Headline)
	}
}

func TestSeverityOKWhenEveryRouteIsFast(t *testing.T) {
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/v1/library": {Count: 100, Buckets: hist(map[string]uint64{"10": 100})},
	}), nil)
	b := newBucket(reader)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	snap := b.Snapshot()

	if snap.Severity != core.SeverityOK {
		t.Errorf("severity = %q, want ok", snap.Severity)
	}
	if snap.Headline != "slowest p99 10 ms — /v1/library" {
		t.Errorf("headline = %q, want the worst p99 and its route", snap.Headline)
	}
}

func TestRouteWithFailingResponsesShowsErrorRate(t *testing.T) {
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/v1/discovery/search": {
			Count:   100,
			Buckets: hist(map[string]uint64{"10": 100}),
			Status:  goapi.StatusClasses{Count2xx: 95, Count5xx: 5},
		},
	}), nil)
	b := newBucket(reader)

	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	d := snapData(t, b.Snapshot())
	if len(d.Routes) != 1 || d.Routes[0].ErrorRate != 0.05 {
		t.Errorf("error_rate = %+v, want 0.05 (5 of 100 responses were 5xx)", d.Routes)
	}
}

func TestHighErrorRateRaisesSeverity(t *testing.T) {
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/v1/discovery/search": {
			Count:   100,
			Buckets: hist(map[string]uint64{"10": 100}),
			Status:  goapi.StatusClasses{Count5xx: 100},
		},
	}), nil)
	b := newBucket(reader)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	snap := b.Snapshot()

	if snap.Severity != core.SeverityCritical {
		t.Errorf("severity = %q, want critical — a fast route serving only 5xx is a failure", snap.Severity)
	}
	if snap.Headline != "error rate 100.0% — /v1/discovery/search" {
		t.Errorf("headline = %q, want the error rate and its route", snap.Headline)
	}
}

func TestSingleErrorOnIdleRouteDoesNotGradeCritical(t *testing.T) {
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/v1/discovery/search": {
			Count:   1,
			Buckets: hist(map[string]uint64{"10": 1}),
			Status:  goapi.StatusClasses{Count5xx: 1},
		},
	}), nil)
	b := newBucket(reader)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	snap := b.Snapshot()

	if snap.Severity != core.SeverityOK {
		t.Errorf("severity = %q, want ok — one 5xx in a one-request window is not evidence", snap.Severity)
	}
	d := snapData(t, snap)
	if len(d.Routes) != 1 || d.Routes[0].ErrorRate != 1 || d.Routes[0].ErrorSamples != 1 {
		t.Errorf("routes = %+v, want the raw 100%% rate over 1 sample still reported", d.Routes)
	}
}

func TestErrorRateGradesCriticalAtTheSampleFloor(t *testing.T) {
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/v1/discovery/search": {
			Count:   minErrorSamples,
			Buckets: hist(map[string]uint64{"10": minErrorSamples}),
			Status:  goapi.StatusClasses{Count5xx: minErrorSamples},
		},
	}), nil)
	b := newBucket(reader)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	snap := b.Snapshot()

	if snap.Severity != core.SeverityCritical {
		t.Errorf("severity = %q, want critical — %d classified responses is a meaningful sample", snap.Severity, minErrorSamples)
	}
	if snap.Headline != "error rate 100.0% — /v1/discovery/search" {
		t.Errorf("headline = %q, want the error rate and its route", snap.Headline)
	}
}

func TestIdleRouteDoesNotHideAFailingBusyRoute(t *testing.T) {
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/health": {
			Count:   1,
			Buckets: hist(map[string]uint64{"10": 1}),
			Status:  goapi.StatusClasses{Count5xx: 1},
		},
		"/v1/discovery/search": {
			Count:   200,
			Buckets: hist(map[string]uint64{"10": 200}),
			Status:  goapi.StatusClasses{Count2xx: 160, Count5xx: 40},
		},
	}), nil)
	b := newBucket(reader)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	snap := b.Snapshot()

	if snap.Severity != core.SeverityCritical {
		t.Errorf("severity = %q, want critical — 40 of 200 responses were 5xx", snap.Severity)
	}
	if snap.Headline != "error rate 20.0% — /v1/discovery/search" {
		t.Errorf("headline = %q, want the busy failing route, not the idle 100%% one", snap.Headline)
	}
}

func TestWindowsErrorRateAcrossSuccessiveReads(t *testing.T) {
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/v1/discovery/search": {
			Count:   1000,
			Buckets: hist(map[string]uint64{"10": 1000}),
			Status:  goapi.StatusClasses{Count5xx: 1000},
		},
	}), nil)
	b := newBucket(reader)

	if err := collectStore(t, b); err != nil {
		t.Fatalf("first Collect: %v", err)
	}
	if snap := b.Snapshot(); snap.Severity != core.SeverityCritical {
		t.Fatalf("first-read severity = %q, want critical (all 5xx so far)", snap.Severity)
	}

	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/v1/discovery/search": {
			Count:   2000,
			Buckets: hist(map[string]uint64{"10": 2000}),
			Status:  goapi.StatusClasses{Count2xx: 1000, Count5xx: 1000},
		},
	}), nil)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("second Collect: %v", err)
	}

	snap := b.Snapshot()
	if snap.Severity != core.SeverityOK {
		t.Errorf("windowed severity = %q, want ok — the recent window is 1000 healthy responses", snap.Severity)
	}
	d := snapData(t, snap)
	if len(d.Routes) != 1 || d.Routes[0].ErrorRate != 0 {
		t.Errorf("windowed error_rate = %+v, want 0 (the delta added no 5xx), not the diluted lifetime 0.5", d.Routes)
	}
}

func TestUnconfiguredDegradesNotCrashes(t *testing.T) {
	b := newBucket(nullReader{})

	if _, err := b.Collect(context.Background()); !goapi.IsSourceDown(err) {
		t.Fatalf("unconfigured Collect error = %v, want source-down", err)
	}
	if snap := b.Snapshot(); snap.State != core.StateSourceDown {
		t.Errorf("unconfigured state = %q, want source_down", snap.State)
	}
}

func TestWindowsLatencyAcrossSuccessiveReads(t *testing.T) {
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/v1/discovery/search": {Count: 1000, Buckets: hist(map[string]uint64{"1000": 1000})},
	}), nil)
	b := newBucket(reader)

	if err := collectStore(t, b); err != nil {
		t.Fatalf("first Collect: %v", err)
	}
	if snap := b.Snapshot(); snap.Severity != core.SeverityCritical {
		t.Fatalf("first-read severity = %q, want critical (all slow so far)", snap.Severity)
	}

	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/v1/discovery/search": {Count: 1100, Buckets: hist(map[string]uint64{"1000": 1000, "10": 100})},
	}), nil)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("second Collect: %v", err)
	}

	snap := b.Snapshot()
	if snap.Severity != core.SeverityOK {
		t.Errorf("windowed severity = %q, want ok — the recent window is 100 fast requests", snap.Severity)
	}
	d := snapData(t, snap)
	if len(d.Routes) != 1 {
		t.Fatalf("routes = %+v, want the one route in the window", d.Routes)
	}
	if d.Routes[0].Count != 100 {
		t.Errorf("windowed count = %d, want 100 (the delta), not the 1100 lifetime total", d.Routes[0].Count)
	}
	if d.Routes[0].P99.Ms >= warnP99Ms {
		t.Errorf("windowed p99 = %v ms, want a fast estimate below the warn band", d.Routes[0].P99.Ms)
	}
}

func TestWindowsTrafficAsDeltaNotLifetime(t *testing.T) {
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/a": {Count: 1000, Buckets: hist(map[string]uint64{"10": 1000})},
	}), nil)
	b := newBucket(reader)

	if err := collectStore(t, b); err != nil {
		t.Fatalf("first Collect: %v", err)
	}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/a": {Count: 1150, Buckets: hist(map[string]uint64{"10": 1150})},
	}), nil)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("second Collect: %v", err)
	}

	trend := snapData(t, b.Snapshot()).Throughput
	latest := trend[len(trend)-1]
	if !strings.Contains(latest.Text, "req/s") {
		t.Errorf("throughput text = %q, want a per-second rate", latest.Text)
	}
	if !strings.Contains(latest.Text, "(150 in window") {
		t.Errorf("throughput text = %q, want the 150-request delta, not the 1150 lifetime total", latest.Text)
	}
}

func TestWindowSurvivesCounterReset(t *testing.T) {
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/a": {Count: 1000, Buckets: hist(map[string]uint64{"10": 1000})},
	}), nil)
	b := newBucket(reader)

	if err := collectStore(t, b); err != nil {
		t.Fatalf("first Collect: %v", err)
	}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/a": {Count: 7, Buckets: hist(map[string]uint64{"10": 7})},
	}), nil)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("post-reset Collect: %v", err)
	}

	d := snapData(t, b.Snapshot())
	if len(d.Routes) != 1 || d.Routes[0].Count != 7 {
		t.Errorf("post-reset windowed count = %+v, want 7 (fresh), never a wrapped underflow", d.Routes)
	}
}
