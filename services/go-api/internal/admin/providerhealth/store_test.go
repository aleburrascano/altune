package providerhealth

import (
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
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
	s.Record(domain.ProviderDiscogs, domain.ProviderStatusOK, 120)
	s.Record(domain.ProviderDiscogs, domain.ProviderStatusCircuitOpen, 0)
	s.Record(domain.ProviderDiscogs, domain.ProviderStatusCircuitOpen, 0)
	s.Record(domain.ProviderDeezer, domain.ProviderStatusOK, 80)

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
	s.Record(domain.ProviderDeezer, domain.ProviderStatusOK, 100)
	s.Record(domain.ProviderDeezer, domain.ProviderStatusOK, 200)

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
		s.Record(domain.ProviderDeezer, domain.ProviderStatusOK, 100)
		s.Record(domain.ProviderDeezer, domain.ProviderStatusOK, 120)

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
		s.Record(domain.ProviderDeezer, domain.ProviderStatusOK, 100)

		// Wall clock steps backward an hour, but ten real minutes elapsed.
		clk.wall = base.Add(-time.Hour)
		clk.elapsed = 10 * time.Minute

		if snap := s.Snapshot(); len(snap) != 0 {
			t.Fatalf("snapshot = %v, want empty (stale must expire despite backward jump)", snap)
		}
	})
}

// TestStore_SnapshotOwnsUpToTheCap pins #2008: past the per-provider sample cap
// the snapshot describes only the retained tail, so it must say so rather than
// pass a capped total off as the whole window.
func TestStore_SnapshotOwnsUpToTheCap(t *testing.T) {
	t.Run("past the cap", func(t *testing.T) {
		s := NewStore()
		const calls = 5000
		for i := 0; i < calls; i++ {
			s.Record(domain.ProviderDeezer, domain.ProviderStatusOK, 100)
		}

		snap := s.Snapshot()
		if len(snap) != 1 {
			t.Fatalf("snapshot len = %d, want 1", len(snap))
		}
		if snap[0].TotalCalls != calls && !snap[0].Truncated {
			t.Errorf("total = %d, truncated = %v; want %d or a set truncation flag",
				snap[0].TotalCalls, snap[0].Truncated, calls)
		}
	})

	t.Run("below the cap", func(t *testing.T) {
		s := NewStore()
		s.Record(domain.ProviderDeezer, domain.ProviderStatusOK, 100)

		if snap := s.Snapshot(); snap[0].Truncated {
			t.Errorf("truncated = true for %d calls, want false", snap[0].TotalCalls)
		}
	})
}

// TestStore_IdleProviderLeavesSnapshot pins #2008: a provider with no live
// samples is reporting nothing, so it must leave the snapshot and release its
// map slots instead of accumulating for the life of the process.
func TestStore_IdleProviderLeavesSnapshot(t *testing.T) {
	clk := &fakeClock{wall: time.Unix(3_000_000, 0).UTC()}
	s := newStoreWithClock(clk.now, clk.since)
	s.Record(domain.ProviderDeezer, domain.ProviderStatusOK, 100)
	s.Record(domain.ProviderDiscogs, domain.ProviderStatusOK, 90)

	clk.elapsed = window + time.Second
	if snap := s.Snapshot(); len(snap) != 0 {
		t.Fatalf("snapshot = %v, want empty once every sample expired", snap)
	}

	if len(s.samples) != 0 || len(s.last) != 0 {
		t.Errorf("retained %d sample slots and %d status slots, want 0 (idle providers must be dropped)",
			len(s.samples), len(s.last))
	}
}
