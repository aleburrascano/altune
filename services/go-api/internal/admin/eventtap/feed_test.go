package eventtap

import (
	"errors"
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
	ch, cancel, err := f.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
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

// TestFeed_SubscribeRejectsPastCeiling pins #996: once MaxSubscribers are live,
// Subscribe refuses the next one without disturbing existing subscribers, and
// cancelling a subscription frees its slot.
func TestFeed_SubscribeRejectsPastCeiling(t *testing.T) {
	f := NewFeed()
	chans := make([]<-chan TapEvent, 0, MaxSubscribers)
	cancels := make([]func(), 0, MaxSubscribers)
	defer func() {
		for _, c := range cancels {
			c()
		}
	}()
	for i := 0; i < MaxSubscribers; i++ {
		ch, cancel, err := f.Subscribe()
		if err != nil {
			t.Fatalf("subscriber %d: %v", i+1, err)
		}
		chans = append(chans, ch)
		cancels = append(cancels, cancel)
	}

	if ch, cancel, err := f.Subscribe(); !errors.Is(err, ErrTooManySubscribers) || ch != nil || cancel != nil {
		t.Fatalf("subscribe past ceiling = (%v, %v, %v), want ErrTooManySubscribers and no channel", ch, cancel != nil, err)
	}

	f.record(TapEvent{Type: "still-live"})
	for i, ch := range chans {
		select {
		case evt := <-ch:
			if evt.Type != "still-live" {
				t.Fatalf("subscriber %d got %q, want still-live", i+1, evt.Type)
			}
		default:
			t.Fatalf("existing subscriber %d stopped receiving after a rejection", i+1)
		}
	}

	cancels[0]()
	cancels[0] = func() {}
	_, cancel, err := f.Subscribe()
	if err != nil {
		t.Fatalf("subscribe after a cancel freed a slot: %v", err)
	}
	cancels = append(cancels, cancel)
}
