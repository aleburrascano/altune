package parallel_test

import (
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/parallel"
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func TestGroupRunsAllWithinLimit(t *testing.T) {
	const limit, n = 3, 50
	var running, peak, done int32
	started := make(chan struct{}, n)
	release := make(chan struct{})
	g := parallel.New("test.fanout", limit)
	submitted := make(chan struct{})
	go func() {
		defer close(submitted)
		for i := 0; i < n; i++ {
			g.Go(context.Background(), func() {
				cur := atomic.AddInt32(&running, 1)
				for {
					p := atomic.LoadInt32(&peak)
					if cur <= p || atomic.CompareAndSwapInt32(&peak, p, cur) {
						break
					}
				}
				started <- struct{}{}
				<-release
				atomic.AddInt32(&done, 1)
				atomic.AddInt32(&running, -1)
			})
		}
	}()

	for i := 0; i < limit; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d tasks started, want %d overlapping", i, limit)
		}
	}
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&running); got != limit {
		t.Fatalf("running = %d while held open, want %d", got, limit)
	}

	close(release)
	<-submitted
	if err := g.Wait(); err != nil {
		t.Fatalf("Wait() = %v, want nil", err)
	}
	if done != n {
		t.Fatalf("done = %d, want %d", done, n)
	}
	if peak != limit {
		t.Fatalf("peak concurrency %d, want %d", peak, limit)
	}
}

func sumOnGroup(t *testing.T, g *parallel.Limited, values []int64) int64 {
	t.Helper()
	var total atomic.Int64
	for _, v := range values {
		g.Go(context.Background(), func() { total.Add(v) })
	}
	if err := g.Wait(); err != nil {
		t.Fatalf("Wait() = %v, want nil", err)
	}
	return total.Load()
}

func TestGroupWaitReturnsAfterEveryTaskHasRun(t *testing.T) {
	values := make([]int64, 200)
	var want int64
	for i := range values {
		values[i] = int64(i + 1)
		want += values[i]
	}
	if got := sumOnGroup(t, parallel.New("test.fanout", 4), values); got != want {
		t.Fatalf("sum after Wait = %d, want %d", got, want)
	}
}

func TestGroupPanickingTaskReachesWaitAsAnErrorAndSiblingsFinish(t *testing.T) {
	ring := captureLogs(t)
	ctx := logging.ContextWithCorrelationID(context.Background(), "corr-fanout-1")
	var finished atomic.Int32
	g := parallel.New("test.fanout_task_panic", 2)
	g.Go(ctx, func() { panic("provider exploded") })
	for range 3 {
		g.Go(ctx, func() { finished.Add(1) })
	}

	err := g.Wait()

	var panicked *background.PanicError
	if !errors.As(err, &panicked) {
		t.Fatalf("Wait() = %v, want a *runloop.PanicError", err)
	}
	if panicked.Value != "provider exploded" || panicked.Event != "test.fanout_task_panic" {
		t.Fatalf("PanicError = {Event: %q, Value: %v}, want the group's event and the panic value", panicked.Event, panicked.Value)
	}
	if got := finished.Load(); got != 3 {
		t.Fatalf("siblings finished = %d, want 3: one panic must not stop the rest", got)
	}
	rec := onlyRecord(t, ring, "test.fanout_task_panic")
	if rec.Attrs["corr_id"] != "corr-fanout-1" {
		t.Errorf("corr_id = %q, want the task's request id on the panic log", rec.Attrs["corr_id"])
	}
}

func TestGroupWaitIsNilWhenNoTaskPanics(t *testing.T) {
	g := parallel.New("test.fanout", 1)
	g.Go(context.Background(), func() {})
	if err := g.Wait(); err != nil {
		t.Fatalf("Wait() = %v, want nil", err)
	}
}

func captureLogs(t *testing.T) *logging.RingBuffer {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	return logging.Setup("error")
}

func onlyRecord(t *testing.T, ring *logging.RingBuffer, msg string) logging.CapturedRecord {
	t.Helper()
	var found []logging.CapturedRecord
	for _, r := range ring.Snapshot() {
		if r.Message == msg {
			found = append(found, r)
		}
	}
	if len(found) != 1 {
		t.Fatalf("captured %d %q records, want exactly 1", len(found), msg)
	}
	return found[0]
}

func TestGroupPanicLogCarriesTheTasksAttributes(t *testing.T) {
	ring := captureLogs(t)
	g := parallel.New("test.fanout_attrs_panic", 1)

	g.Go(context.Background(), func() { panic("boom") }, "provider", "deezer")
	_ = g.Wait()

	if got := onlyRecord(t, ring, "test.fanout_attrs_panic").Attrs["provider"]; got != "deezer" {
		t.Errorf("provider = %q, want the attribute the task was started with", got)
	}
}
