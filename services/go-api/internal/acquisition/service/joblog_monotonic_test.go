package service

import (
	"altune/go-api/internal/acquisition/ports"
	"testing"
	"time"
)

type jobClock struct {
	wall    time.Time
	elapsed time.Duration
}

func (c *jobClock) now() time.Time                { return c.wall }
func (c *jobClock) since(time.Time) time.Duration { return c.elapsed }

func TestJobLog_ElapsedImmuneToWallClockJump(t *testing.T) {
	base := time.Unix(3_000_000, 0)

	t.Run("complete after backward wall step", func(t *testing.T) {
		clk := &jobClock{wall: base, elapsed: 0}
		l := newJobLogWithClock(clk.now, clk.since)
		l.register("trk-1", "https://src.example/one")

		clk.wall = base.Add(-time.Minute)
		clk.elapsed = 2 * time.Second

		l.complete("trk-1", ports.JobSucceeded, "")

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
