package service

import (
	"testing"
	"time"
)

// jobClock drives the now/since seams independently so a test can diverge wall
// time (now) from monotonic elapsed (since) the way an OS clock step does.
type jobClock struct {
	wall    time.Time
	elapsed time.Duration
}

func (c *jobClock) now() time.Time                { return c.wall }
func (c *jobClock) since(time.Time) time.Duration { return c.elapsed }

// TestJobLog_ElapsedImmuneToWallClockJump pins the bug fix: elapsed time must be
// judged by monotonic elapsed (since), not a wall-clock subtraction. A backward
// wall step while a job is in flight must never produce a negative ElapsedMs.
func TestJobLog_ElapsedImmuneToWallClockJump(t *testing.T) {
	base := time.Unix(3_000_000, 0)

	t.Run("complete after backward wall step", func(t *testing.T) {
		clk := &jobClock{wall: base, elapsed: 0}
		l := newJobLogWithClock(clk.now, clk.since)
		l.register("trk-1", "https://src.example/one")

		// Wall clock steps backward a minute, but two real seconds elapsed.
		clk.wall = base.Add(-time.Minute)
		clk.elapsed = 2 * time.Second

		l.complete("trk-1", JobSucceeded, "")

		_, recent := l.snapshot()
		if len(recent) != 1 {
			t.Fatalf("recent = %d, want 1", len(recent))
		}
		if got := recent[0].ElapsedMs; got != 2000 {
			t.Fatalf("ElapsedMs = %d, want 2000 (backward wall step must not make elapsed negative/bogus)", got)
		}
	})

	t.Run("in-flight snapshot after backward wall step", func(t *testing.T) {
		clk := &jobClock{wall: base, elapsed: 0}
		l := newJobLogWithClock(clk.now, clk.since)
		l.register("trk-2", "https://src.example/two")

		clk.wall = base.Add(-time.Minute)
		clk.elapsed = 3 * time.Second

		jobs, _ := l.snapshot()
		if len(jobs) != 1 {
			t.Fatalf("jobs = %d, want 1", len(jobs))
		}
		if got := jobs[0].ElapsedMs; got != 3000 {
			t.Fatalf("ElapsedMs = %d, want 3000 (in-flight elapsed must track monotonic, not wall)", got)
		}
	})
}
