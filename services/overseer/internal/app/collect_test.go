package app

import (
	"altune/overseer/internal/config"
	"altune/overseer/internal/core"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"
)

// newTestApp wires an App with only what the collect loop needs: a registry and the
// two durations that bound a cycle. Everything else (server, verifier, SPA) belongs
// to the HTTP surface, which these tests never raise.
func newTestApp(reg *core.Registry, tickInterval, bucketTimeout time.Duration) *App {
	return &App{
		cfg:      &config.Config{TickInterval: tickInterval, BucketTimeout: bucketTimeout},
		registry: reg,
		down:     map[string]bool{},
	}
}

// panicBucket panics in Collect and Store to prove the collect loop contains a
// misbehaving bucket instead of crashing the process — the degrade-don't-crash
// invariant on the collect side, matching shell.safeSnapshot on the render side.
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

// stubBucket collects cleanly, or returns collectErr to model a down source, so a
// cycle can be driven with a known mix of healthy and failing buckets.
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

// toggleBucket models a source that goes down and later recovers within one test:
// Collect fails while *down is true, so a single instance drives a whole outage and
// its recovery across successive ticks.
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

// stalledBucket models the failure the per-bucket deadline exists for: a Collect
// that waits on a source which never answers. It returns only when its context ends
// and closes cancelled to prove the deadline, not the source, freed the loop.
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

// storingBucket closes stored when its Store runs, so a test can prove a bucket
// queued behind a stalled one still completed its own cycle.
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

// isClosed reports whether ch was closed, without waiting.
func isClosed(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// captureSlog redirects the default logger into a buffer for the duration of the
// test, restoring the previous logger afterwards so the process-wide logger is
// left as it was found.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	// DEBUG so the heartbeat, demoted from INFO once /health owns liveness, is still
	// captured by the count assertions below.
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// linesFor returns every captured log record whose msg matches, in emission order.
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

// findCycle returns the fields of the single "overseer.collect.cycle" heartbeat in
// buf, failing if none was emitted.
func findCycle(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := linesFor(t, buf, "overseer.collect.cycle")
	if len(lines) == 0 {
		t.Fatal("collectAll emitted no overseer.collect.cycle heartbeat")
	}
	return lines[0]
}

// TestCollectAllHeartbeatCounts: a cycle over one healthy and one down bucket must
// close with an overseer.collect.cycle heartbeat reporting ok=1, failed=1 — proving
// the loop ran and separating a partial failure (still ok>=1) from an all-down
// cycle. Without this heartbeat success is silent and the smoke gate can only test
// for the ABSENCE of errors.
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

// TestCollectAllCountsStorePanicAsFailed: a bucket that collects cleanly but panics
// in Store must land under failed, never ok. It is the one failure the heartbeat
// could flatter: Collect succeeded, so a safeStore that swallowed the panic and
// reported success would emit ok=2 failed=0 and the smoke gate would read a cycle
// that stored nothing as fully healthy.
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

// TestSafeCollectContainsPanic: a bucket panicking in Collect yields an error,
// not a process-killing panic.
func TestSafeCollectContainsPanic(t *testing.T) {
	signals, err := safeCollect(context.Background(), panicBucket{where: "collect"})
	if err == nil {
		t.Fatal("safeCollect returned nil error for a panicking bucket; the panic was not contained")
	}
	if signals != nil {
		t.Errorf("safeCollect returned signals %v after a panic; want nil", signals)
	}
}

// TestCollectAllDeadlineFreesTheCycleFromAStalledBucket: a bucket whose Collect
// never returns on its own is cancelled at the per-bucket deadline, and the bucket
// behind it in the same serial cycle still collects and stores. Without the deadline
// the stalled bucket holds the one tickLoop goroutine forever: every later bucket
// goes stale and no further cycle ever runs.
func TestCollectAllDeadlineFreesTheCycleFromAStalledBucket(t *testing.T) {
	// The registry orders by ID, so "a-stalled" runs before "z-healthy" — the
	// starved-sibling case, not the lucky order.
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

// TestCollectStatusIsUnhealthyUntilACycleCompletes: /health must not read green off
// a loop that has never completed a cycle. Run collects once synchronously before the
// listener opens, so a serving Overseer with no recorded cycle never started one.
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

// TestCollectStatusGoesUnhealthyOnAStalledLoop: once the last completed cycle is
// older than the staleness budget — the slowest cycle the config allows plus a few
// missed ticks — the loop counts as wedged and /health must be able to say so.
func TestCollectStatusGoesUnhealthyOnAStalledLoop(t *testing.T) {
	reg := core.NewRegistry()
	reg.Register(stubBucket{id: "healthy"})
	// One bucket at a 1ms deadline and a 1ms tick: a budget of 4ms, so the sleep
	// below outlives it by an order of magnitude on any machine.
	a := newTestApp(reg, time.Millisecond, time.Millisecond)
	captureSlog(t)
	a.collectAll(context.Background())

	time.Sleep(40 * time.Millisecond)

	if status := a.collectStatus(); status.Healthy {
		t.Errorf("collectStatus is healthy %v after the last cycle, with a %v budget; a wedged loop reports green", time.Since(status.LastCycle), a.stalenessBudget(status.OK+status.Failed))
	}
}

// TestSafeStoreContainsPanic: a bucket panicking in Store must not escape; it is
// returned as an error so the cycle counts it as failed instead of crashing.
func TestSafeStoreContainsPanic(t *testing.T) {
	err := safeStore(panicBucket{where: "store"}, []core.Signal{{Text: "x"}})
	if err == nil {
		t.Fatal("safeStore returned nil for a panicking Store; the panic was not contained")
	}
}

// TestSustainedOutageLogsOneTransitionNotAFloodPerTick: a source that stays down
// across many ticks logs exactly one source_down line, then exactly one source_up
// when it recovers — the incident, not a WARN-per-tick flood over the whole outage.
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

// TestBucketPanicLogsOnceAtErrorNotWarn: a bucket that panics every tick is logged
// once at ERROR as a crash, never re-logged at WARN as a down source — a real crash
// must not be downgraded, nor flood ERROR every tick.
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
