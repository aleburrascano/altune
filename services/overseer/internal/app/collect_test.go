package app

import (
	"altune/overseer/internal/config"
	"altune/overseer/internal/core"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func newTestApp(reg *core.Registry, tickInterval, bucketTimeout time.Duration) *App {
	return &App{
		cfg:      &config.Config{TickInterval: tickInterval, BucketTimeout: bucketTimeout},
		registry: reg,
		down:     map[string]bool{},
	}
}

type panicBucket struct{ where string }

func (panicBucket) Meta() core.Meta { return core.Meta{ID: "boom", Title: "Boom"} }
func (p panicBucket) Collect(context.Context) ([]core.Signal, error) {
	if p.where == "collect" {
		panic("collect blew up")
	}
	return []core.Signal{{Text: "ok"}}, nil
}

func (p panicBucket) Store([]core.Signal) {
	if p.where == "store" {
		panic("store blew up")
	}
}
func (panicBucket) Snapshot() core.Snapshot { return core.Snapshot{} }

type stubBucket struct {
	id         string
	collectErr error
}

func (s stubBucket) Meta() core.Meta { return core.Meta{ID: s.id} }

func (s stubBucket) Collect(context.Context) ([]core.Signal, error) {
	if s.collectErr != nil {
		return nil, s.collectErr
	}
	return []core.Signal{{Text: "ok"}}, nil
}

func (stubBucket) Store([]core.Signal)     {}
func (stubBucket) Snapshot() core.Snapshot { return core.Snapshot{} }

type toggleBucket struct {
	id   string
	down *bool
}

func (b *toggleBucket) Meta() core.Meta { return core.Meta{ID: b.id} }

func (b *toggleBucket) Collect(context.Context) ([]core.Signal, error) {
	if *b.down {
		return nil, errors.New("source down")
	}
	return []core.Signal{{Text: "ok"}}, nil
}

func (*toggleBucket) Store([]core.Signal)     {}
func (*toggleBucket) Snapshot() core.Snapshot { return core.Snapshot{} }

type stalledBucket struct {
	id        string
	cancelled chan struct{}
}

func newStalledBucket(id string) *stalledBucket {
	return &stalledBucket{id: id, cancelled: make(chan struct{})}
}

func (s *stalledBucket) Meta() core.Meta { return core.Meta{ID: s.id} }

func (s *stalledBucket) Collect(ctx context.Context) ([]core.Signal, error) {
	<-ctx.Done()
	close(s.cancelled)
	return nil, ctx.Err()
}

func (*stalledBucket) Store([]core.Signal)     {}
func (*stalledBucket) Snapshot() core.Snapshot { return core.Snapshot{} }

type storingBucket struct {
	id     string
	stored chan struct{}
}

func newStoringBucket(id string) *storingBucket {
	return &storingBucket{id: id, stored: make(chan struct{})}
}

func (s *storingBucket) Meta() core.Meta { return core.Meta{ID: s.id} }

func (*storingBucket) Collect(context.Context) ([]core.Signal, error) {
	return []core.Signal{{Text: "ok"}}, nil
}

func (s *storingBucket) Store([]core.Signal)   { close(s.stored) }
func (*storingBucket) Snapshot() core.Snapshot { return core.Snapshot{} }

type schedulerBucket struct {
	refreshes *atomic.Int32
	stopped   chan struct{}
}

func (schedulerBucket) Meta() core.Meta                                { return core.Meta{ID: "scheduler"} }
func (schedulerBucket) Collect(context.Context) ([]core.Signal, error) { return nil, nil }
func (schedulerBucket) Store([]core.Signal)                            {}
func (schedulerBucket) Snapshot() core.Snapshot                        { return core.Snapshot{} }

func (s schedulerBucket) Start(ctx context.Context) {
	go func() {
		defer close(s.stopped)
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.refreshes.Add(1)
			}
		}
	}()
}

func eventually(t *testing.T, within time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.After(within)
	for {
		if cond() {
			return
		}
		select {
		case <-deadline:
			t.Fatal(msg)
		case <-time.After(time.Millisecond):
		}
	}
}

func isClosed(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func linesFor(t *testing.T, buf *bytes.Buffer, msg string) []map[string]any {
	t.Helper()
	var matched []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var rec map[string]any
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec["msg"] == msg {
			matched = append(matched, rec)
		}
	}
	return matched
}

func findCycle(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := linesFor(t, buf, "overseer.collect.cycle")
	if len(lines) == 0 {
		t.Fatal("collectAll emitted no overseer.collect.cycle heartbeat")
	}
	return lines[0]
}

func TestCollectAllHeartbeatCounts(t *testing.T) {
	reg := core.NewRegistry()
	reg.Register(stubBucket{id: "healthy"})
	reg.Register(stubBucket{id: "down", collectErr: errors.New("source down")})

	buf := captureSlog(t)

	newTestApp(reg, time.Second, time.Second).collectAll(context.Background())

	rec := findCycle(t, buf)
	if rec["ok"] != float64(1) || rec["failed"] != float64(1) {
		t.Errorf("heartbeat = ok:%v failed:%v; want ok:1 failed:1", rec["ok"], rec["failed"])
	}
}

func TestCollectAllCountsStorePanicAsFailed(t *testing.T) {
	reg := core.NewRegistry()
	reg.Register(stubBucket{id: "healthy"})
	reg.Register(panicBucket{where: "store"})

	buf := captureSlog(t)

	newTestApp(reg, time.Second, time.Second).collectAll(context.Background())

	rec := findCycle(t, buf)
	if rec["ok"] != float64(1) || rec["failed"] != float64(1) {
		t.Errorf("heartbeat = ok:%v failed:%v; want ok:1 failed:1 (the store-panicking bucket counted as failed)", rec["ok"], rec["failed"])
	}
}

func TestStartBucketsDrivesBackgroundWorkPastFirstRefreshThenStops(t *testing.T) {
	var refreshes atomic.Int32
	bucket := schedulerBucket{refreshes: &refreshes, stopped: make(chan struct{})}
	reg := core.NewRegistry()
	reg.Register(bucket)
	a := newTestApp(reg, time.Second, time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	a.startBuckets(ctx)

	eventually(t, 2*time.Second, func() bool { return refreshes.Load() >= 2 },
		"background loop fired fewer than 2 refreshes through the Start hook — it froze after one, the #1812 regression")

	cancel()
	select {
	case <-bucket.stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("background loop did not drain within 2s of ctx cancel — goroutine leaked past shutdown")
	}
}

func TestStartBucketsSkipsBucketsWithoutTheHook(t *testing.T) {
	reg := core.NewRegistry()
	reg.Register(stubBucket{id: "plain"})
	a := newTestApp(reg, time.Second, time.Second)

	a.startBuckets(context.Background())

	captureSlog(t)
	a.collectAll(context.Background())
	if status := a.collectStatus(); status.OK != 1 {
		t.Errorf("plain bucket did not collect after startBuckets skipped it: ok=%d, want 1", status.OK)
	}
}

func TestSafeCollectContainsPanic(t *testing.T) {
	signals, err := safeCollect(context.Background(), panicBucket{where: "collect"})
	if err == nil {
		t.Fatal("safeCollect returned nil error for a panicking bucket; the panic was not contained")
	}
	if signals != nil {
		t.Errorf("safeCollect returned signals %v after a panic; want nil", signals)
	}
}

func TestCollectAllDeadlineFreesTheCycleFromAStalledBucket(t *testing.T) {
	stalled := newStalledBucket("a-stalled")
	healthy := newStoringBucket("z-healthy")
	reg := core.NewRegistry()
	reg.Register(stalled)
	reg.Register(healthy)
	buf := captureSlog(t)

	newTestApp(reg, time.Second, 20*time.Millisecond).collectAll(context.Background())

	if !isClosed(stalled.cancelled) {
		t.Error("the stalled bucket's Collect was not cancelled; the per-bucket deadline did not fire")
	}
	if !isClosed(healthy.stored) {
		t.Error("the bucket queued behind the stalled one never stored; the cycle was starved")
	}
	rec := findCycle(t, buf)
	if rec["ok"] != float64(1) || rec["failed"] != float64(1) {
		t.Errorf("heartbeat = ok:%v failed:%v; want ok:1 failed:1 (the stalled bucket failed, its sibling did not)", rec["ok"], rec["failed"])
	}
}

func TestCollectStatusIsUnhealthyUntilACycleCompletes(t *testing.T) {
	reg := core.NewRegistry()
	reg.Register(stubBucket{id: "healthy"})
	a := newTestApp(reg, time.Second, time.Second)

	if status := a.collectStatus(); status.Healthy {
		t.Error("collectStatus is healthy before any cycle ran; a dead loop would report green")
	}

	captureSlog(t)
	a.collectAll(context.Background())

	status := a.collectStatus()
	if !status.Healthy {
		t.Error("collectStatus is unhealthy right after a completed cycle")
	}
	if status.OK != 1 || status.Failed != 0 {
		t.Errorf("collectStatus counts = ok:%d failed:%d; want ok:1 failed:0", status.OK, status.Failed)
	}
	if status.LastCycle.IsZero() {
		t.Error("collectStatus reports no last cycle after one completed")
	}
}

func TestCollectStatusGoesUnhealthyOnAStalledLoop(t *testing.T) {
	reg := core.NewRegistry()
	reg.Register(stubBucket{id: "healthy"})
	a := newTestApp(reg, time.Millisecond, time.Millisecond)
	captureSlog(t)
	a.collectAll(context.Background())

	time.Sleep(40 * time.Millisecond)

	if status := a.collectStatus(); status.Healthy {
		t.Errorf("collectStatus is healthy %v after the last cycle, with a %v budget; a wedged loop reports green", time.Since(status.LastCycle), a.stalenessBudget(status.OK+status.Failed))
	}
}

func TestSafeStoreContainsPanic(t *testing.T) {
	err := safeStore(panicBucket{where: "store"}, []core.Signal{{Text: "x"}})
	if err == nil {
		t.Fatal("safeStore returned nil for a panicking Store; the panic was not contained")
	}
}

func TestSustainedOutageLogsOneTransitionNotAFloodPerTick(t *testing.T) {
	sourceDown := true
	reg := core.NewRegistry()
	reg.Register(&toggleBucket{id: "goapi", down: &sourceDown})
	a := newTestApp(reg, time.Second, time.Second)
	buf := captureSlog(t)

	const ticks = 5
	for range ticks {
		a.collectAll(context.Background())
	}
	sourceDown = false
	a.collectAll(context.Background())

	if got := len(linesFor(t, buf, "overseer.collect.source_down")); got != 1 {
		t.Errorf("a %d-tick outage logged source_down %d times; want 1 transition, not a per-tick flood", ticks, got)
	}
	if got := len(linesFor(t, buf, "overseer.collect.source_up")); got != 1 {
		t.Errorf("recovery logged source_up %d times; want exactly 1", got)
	}
}

func TestBucketPanicLogsOnceAtErrorNotWarn(t *testing.T) {
	reg := core.NewRegistry()
	reg.Register(panicBucket{where: "collect"})
	a := newTestApp(reg, time.Second, time.Second)
	buf := captureSlog(t)

	for range 3 {
		a.collectAll(context.Background())
	}

	panics := linesFor(t, buf, "overseer.collect.bucket_panic")
	if len(panics) != 1 {
		t.Fatalf("a panicking bucket logged bucket_panic %d times over 3 ticks; want 1", len(panics))
	}
	if panics[0]["level"] != "ERROR" {
		t.Errorf("bucket_panic logged at level %v; want ERROR", panics[0]["level"])
	}
	if got := len(linesFor(t, buf, "overseer.collect.source_down")); got != 0 {
		t.Errorf("a panic was re-logged as source_down %d times; a crash must not be downgraded to WARN", got)
	}
}
