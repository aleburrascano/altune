package eventtap

import (
	"testing"
	"time"
)

// fakeClock drives the now/since seams independently so a test can diverge
// wall time (now) from monotonic elapsed (since) the way an OS clock step does.
type fakeClock struct {
	wall    time.Time                     // value returned by now(), used to stamp
	elapsed func(time.Time) time.Duration // monotonic elapsed returned by since()
}

func (c *fakeClock) now() time.Time { return c.wall }
func (c *fakeClock) since(t time.Time) time.Duration {
	return c.elapsed(t)
}

func TestFeed_Rates(t *testing.T) {
	base := time.Unix(1_000_000, 0).UTC()
	clk := &fakeClock{wall: base}
	clk.elapsed = func(t time.Time) time.Duration { return clk.wall.Sub(t) }
	f := newFeedWithClock(clk.now, clk.since)

	// An older search, then two minutes of monotonic time, then the recent batch.
	f.record(TapEvent{Type: "search"})
	clk.wall = base.Add(2 * time.Minute)
	f.record(TapEvent{Type: "search"})
	f.record(TapEvent{Type: "search"})
	f.record(TapEvent{Type: "track_added"})

	rates := f.Rates()
	if rates["search"] != 2 {
		t.Errorf("search rate = %d, want 2 (stale pruned)", rates["search"])
	}
	if rates["track_added"] != 1 {
		t.Errorf("track_added rate = %d, want 1", rates["track_added"])
	}
}

func TestFeed_FanOutToSubscribers(t *testing.T) {
	f := NewFeed()
	ch, cancel := f.Subscribe()
	defer cancel()

	f.record(TapEvent{Type: "live", Timestamp: time.Now().UTC()})

	select {
	case evt := <-ch:
		if evt.Type != "live" {
			t.Errorf("type = %q, want live", evt.Type)
		}
	default:
		t.Fatal("subscriber did not receive the event")
	}
}

// TestFeed_RatesImmuneToWallClockJump pins the bug fix: pruning must follow
// monotonic elapsed (since), never the absolute wall reading (now). A forward
// wall jump with little real time elapsed must not wipe buffered samples; a
// backward jump after real time elapsed must still expire stale ones.
func TestFeed_RatesImmuneToWallClockJump(t *testing.T) {
	base := time.Unix(2_000_000, 0).UTC()

	t.Run("forward jump keeps fresh samples", func(t *testing.T) {
		clk := &fakeClock{wall: base, elapsed: func(time.Time) time.Duration { return 0 }}
		f := newFeedWithClock(clk.now, clk.since)
		f.record(TapEvent{Type: "search"})
		f.record(TapEvent{Type: "search"})

		// Wall clock steps forward an hour; only a second of real time passed.
		clk.wall = base.Add(time.Hour)
		clk.elapsed = func(time.Time) time.Duration { return time.Second }

		if got := f.Rates()["search"]; got != 2 {
			t.Errorf("search rate = %d, want 2 (forward wall jump must not prune fresh samples)", got)
		}
	})

	t.Run("backward jump still expires stale samples", func(t *testing.T) {
		clk := &fakeClock{wall: base, elapsed: func(time.Time) time.Duration { return 0 }}
		f := newFeedWithClock(clk.now, clk.since)
		f.record(TapEvent{Type: "search"})

		// Wall clock steps backward an hour, but ten real minutes elapsed.
		clk.wall = base.Add(-time.Hour)
		clk.elapsed = func(time.Time) time.Duration { return 10 * time.Minute }

		if got := f.Rates()["search"]; got != 0 {
			t.Errorf("search rate = %d, want 0 (stale sample must expire despite backward wall jump)", got)
		}
	})
}
