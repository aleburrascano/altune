package security

import (
	"context"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

type panicProber struct{}

func (panicProber) target(string, string) *url.URL { return &url.URL{Host: "x"} }
func (panicProber) do(context.Context, *url.URL) probeResult {
	panic("self-test probe blew up")
}

func TestSchedulerContainsProbePanic(t *testing.T) {
	s := newScheduler(panicProber{}, defaultSuite(), time.Hour, func(suiteResult) {})

	s.safeRunOnce(context.Background())

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

func TestSchedulerClampsInterval(t *testing.T) {
	s := newScheduler(nullProber{}, defaultSuite(), 0, func(suiteResult) {})
	if s.interval != defaultInterval {
		t.Errorf("interval = %v, want clamped to %v", s.interval, defaultInterval)
	}
}
