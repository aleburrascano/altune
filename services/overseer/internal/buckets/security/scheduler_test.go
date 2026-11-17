package security_test

import (
	"altune/overseer/internal/buckets/security"
	"context"
	"testing"
	"time"
)

const settle = 50 * time.Millisecond

func TestSchedulerContainsProbePanic(t *testing.T) {
	b := runScripted(t, panics(), status(401))

	if d := snapData(t, b.Snapshot()); !d.HasRun || d.Passed != suiteSize {
		t.Fatalf("data = %+v after a panicking run, want the next run recorded", d)
	}
}

func TestSchedulerExitsOnCancel(t *testing.T) {
	s := newScript(status(401))
	b := security.New(security.Deps{Prober: s, Interval: time.Millisecond, Now: s.now})
	ctx, cancel := context.WithCancel(context.Background())
	b.Start(ctx)
	waitFor(t, func() bool { return s.started() >= 2 }, "the scheduler to tick")

	cancel()
	time.Sleep(settle)
	stopped := s.started()
	time.Sleep(settle)

	if got := s.started(); got != stopped {
		t.Fatalf("scheduler ran %d more time(s) after ctx cancel — goroutine leak", got-stopped)
	}
}

func TestANonPositiveIntervalFallsBackToTheDefault(t *testing.T) {
	for _, interval := range []time.Duration{0, -time.Second} {
		s := newScript(status(401))
		startBucket(t, s, interval)
		waitFor(t, func() bool { return s.started() >= 1 }, "the immediate run")

		time.Sleep(settle)
		if got := s.started(); got != 1 {
			t.Errorf("interval %v ran %d times within %v, want the hourly default", interval, got, settle)
		}
	}
}

func TestStartingTwiceRunsOneScheduler(t *testing.T) {
	s := newScript(status(401))
	b, _ := startBucket(t, s, time.Hour)
	b.Start(context.Background())
	waitFor(t, func() bool { return s.started() >= 1 }, "the immediate run")

	time.Sleep(settle)
	if got := s.started(); got != 1 {
		t.Errorf("two Starts ran the suite %d times, want once", got)
	}
}
