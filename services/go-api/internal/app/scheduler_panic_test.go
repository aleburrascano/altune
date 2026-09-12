package app

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestRunTicker_RecoversFromPanickingJob is the regression for #392: scheduled
// jobs run in their own goroutines outside any HTTP request, so the
// httputil.Recoverer that guards request handlers cannot catch them. Without a
// recover() of its own, a single panicking tick terminates the whole process
// and takes down live traffic. The tick must be contained: the panic is
// logged (naming the job) and the loop survives to tick again.
func TestRunTicker_RecoversFromPanickingJob(t *testing.T) {
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError})))
	defer slog.SetDefault(restore)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int32
	ran := make(chan struct{}, 8)
	a := &App{}
	a.runTicker(ctx, "explode", time.Millisecond, func() {
		n := calls.Add(1)
		ran <- struct{}{}
		if n == 1 {
			panic("boom")
		}
	})

	<-ran // first (immediate) invocation panics; without recover() this kills the process
	<-ran // a second tick proves the goroutine survived the panic
	cancel()

	logged := buf.String()
	if !strings.Contains(logged, "explode") {
		t.Errorf("panic log did not name the job, got: %q", logged)
	}
	if !strings.Contains(logged, "boom") {
		t.Errorf("panic log did not include the panic value, got: %q", logged)
	}
}

// TestWhenLeaderJobs_RecoverOnAcquire guards the leader-acquire path: a job
// that panics the moment leadership is acquired must not escape the shared
// goroutine and crash the process, and must not prevent the jobs registered
// after it from starting.
func TestWhenLeaderJobs_RecoverOnAcquire(t *testing.T) {
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError})))
	defer slog.SetDefault(restore)

	a := &App{}
	var secondRan atomic.Bool
	a.whenLeader("panicky", func(context.Context) { panic("on-acquire boom") })
	a.whenLeader("survivor", func(context.Context) { secondRan.Store(true) })

	for _, job := range a.backgroundStarts {
		guard(job.name, func() { job.start(context.Background()) })
	}

	if !secondRan.Load() {
		t.Fatal("a panic in one leader job prevented the next job from starting")
	}
	logged := buf.String()
	if !strings.Contains(logged, "panicky") {
		t.Errorf("panic log did not name the job, got: %q", logged)
	}
}
