package app

import (
	"altune/overseer/internal/core"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
)

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

// captureSlog redirects the default logger into a buffer for the duration of the
// test, restoring the previous logger afterwards so the process-wide logger is
// left as it was found.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// findCycle returns the fields of the single "overseer.collect.cycle" heartbeat in
// buf, failing if none was emitted — the positive signal the smoke gate reads.
func findCycle(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var rec map[string]any
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec["msg"] == "overseer.collect.cycle" {
			return rec
		}
	}
	t.Fatal("collectAll emitted no overseer.collect.cycle heartbeat")
	return nil
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

	(&App{registry: reg}).collectAll(context.Background())

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

	(&App{registry: reg}).collectAll(context.Background())

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

// TestSafeStoreContainsPanic: a bucket panicking in Store must not escape.
func TestSafeStoreContainsPanic(t *testing.T) {
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("safeStore let a bucket panic escape: %v", rec)
		}
	}()
	safeStore(context.Background(), panicBucket{where: "store"}, []core.Signal{{Text: "x"}})
}
