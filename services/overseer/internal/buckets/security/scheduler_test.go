package security

import (
	"context"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

// panicProber panics on every probe, standing in for a misbehaving self-test.
type panicProber struct{}

func (panicProber) target(string, string) *url.URL { return &url.URL{Host: "x"} }
func (panicProber) do(context.Context, *url.URL) probeResult {
	panic("self-test probe blew up")
}

// TestSchedulerContainsProbePanic is the panic-containment proof for the
// scheduler's background goroutine: a probe that panics must be contained — the
// process survives and the scheduler keeps running so the next run executes.
// Without the recover in safeRunOnce this would crash the whole Overseer
// process, since the run loop lives outside the shell's safeCollect recover.
func TestSchedulerContainsProbePanic(t *testing.T) {
	s := newScheduler(panicProber{}, defaultSuite(), time.Hour, func(suiteResult) {})

	// A direct panicking run is contained, not propagated.
	s.safeRunOnce(context.Background())

	// The scheduler still runs: tick fast through several panicking runs, then
	// cancel — it must return cleanly rather than having crashed.
	s = newScheduler(panicProber{}, defaultSuite(), time.Millisecond, func(suiteResult) {})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.run(ctx); close(done) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler.run did not return after panicking probes — panic escaped containment")
	}
}

// TestSchedulerExitsOnCancel proves the background goroutine drains on ctx
// cancellation — no goroutine leaks past the app's lifetime.
func TestSchedulerExitsOnCancel(t *testing.T) {
	var runs atomic.Int32
	s := newScheduler(nullProber{}, defaultSuite(), time.Millisecond,
		func(suiteResult) { runs.Add(1) })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.run(ctx); close(done) }()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler.run did not return within 2s of ctx cancel — goroutine leak")
	}
	if runs.Load() == 0 {
		t.Error("scheduler never ran the immediate pass")
	}
}

// TestSchedulerClampsInterval proves a non-positive interval cannot disable the
// ticker: it is clamped to the default rather than panicking NewTicker.
func TestSchedulerClampsInterval(t *testing.T) {
	s := newScheduler(nullProber{}, defaultSuite(), 0, func(suiteResult) {})
	if s.interval != defaultInterval {
		t.Errorf("interval = %v, want clamped to %v", s.interval, defaultInterval)
	}
}
