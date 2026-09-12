package providerhealth

import (
	"testing"
	"time"
)

// fakeClock drives the now/since seams independently so a test can diverge
// wall time (now) from monotonic elapsed (since) the way an OS clock step does.
type fakeClock struct {
	wall    time.Time
	elapsed time.Duration
}

func (c *fakeClock) now() time.Time                { return c.wall }
func (c *fakeClock) since(time.Time) time.Duration { return c.elapsed }

func TestStore_StatusMixAndCurrent(t *testing.T) {
	s := NewStore()
	s.Record("discogs", "ok", 120)
	s.Record("discogs", "circuit_open", 0)
	s.Record("discogs", "circuit_open", 0)
	s.Record("deezer", "ok", 80)

	snap := s.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("snapshot len = %d, want 2", len(snap))
	}

	if snap[0].Provider != "deezer" || snap[1].Provider != "discogs" {
		t.Fatalf("providers not ordered by name: %s, %s", snap[0].Provider, snap[1].Provider)
	}

	discogs := snap[1]
	if discogs.CurrentStatus != "circuit_open" {
		t.Errorf("current = %q, want circuit_open (most recent)", discogs.CurrentStatus)
	}
	if discogs.CountsPerStatus["circuit_open"] != 2 || discogs.CountsPerStatus["ok"] != 1 {
		t.Errorf("counts = %v, want 2 circuit_open + 1 ok", discogs.CountsPerStatus)
	}
	if discogs.TotalCalls != 3 {
		t.Errorf("total = %d, want 3", discogs.TotalCalls)
	}
}

func TestStore_AvgLatency(t *testing.T) {
	s := NewStore()
	s.Record("deezer", "ok", 100)
	s.Record("deezer", "ok", 200)

	snap := s.Snapshot()
	if snap[0].AvgLatencyMs != 150 {
		t.Errorf("avg latency = %d, want 150", snap[0].AvgLatencyMs)
	}
}

// TestStore_PruningImmuneToWallClockJump pins the bug fix: the window must be
// judged by monotonic elapsed (since), not the absolute wall reading (now). A
// forward wall jump with little real time elapsed must not drop fresh samples;
// a backward jump after real time elapsed must still expire stale ones.
func TestStore_PruningImmuneToWallClockJump(t *testing.T) {
	base := time.Unix(3_000_000, 0).UTC()

	t.Run("forward jump keeps fresh samples", func(t *testing.T) {
		clk := &fakeClock{wall: base, elapsed: 0}
		s := newStoreWithClock(clk.now, clk.since)
		s.Record("deezer", "ok", 100)
		s.Record("deezer", "ok", 120)

		// Wall clock steps forward an hour; only a second of real time passed.
		clk.wall = base.Add(time.Hour)
		clk.elapsed = time.Second

		snap := s.Snapshot()
		if len(snap) != 1 || snap[0].TotalCalls != 2 {
			t.Fatalf("total calls = %v, want a single provider with 2 (forward jump must not prune fresh samples)", snap)
		}
	})

	t.Run("backward jump still expires stale samples", func(t *testing.T) {
		clk := &fakeClock{wall: base, elapsed: 0}
		s := newStoreWithClock(clk.now, clk.since)
		s.Record("deezer", "ok", 100)

		// Wall clock steps backward an hour, but ten real minutes elapsed.
		clk.wall = base.Add(-time.Hour)
		clk.elapsed = 10 * time.Minute

		snap := s.Snapshot()
		if len(snap) != 1 || snap[0].TotalCalls != 0 {
			t.Fatalf("total calls = %v, want a single provider with 0 (stale must expire despite backward jump)", snap)
		}
	})
}
