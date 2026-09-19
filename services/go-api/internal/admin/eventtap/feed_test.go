package eventtap

import (
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
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

// TestFeed_RatesExactUnderBurst pins #2008: a burst far larger than the
// window's internal buffer must report its true count, not the buffer's size,
// and must still hold a bounded number of buckets.
func TestFeed_RatesExactUnderBurst(t *testing.T) {
	base := time.Unix(4_000_000, 0).UTC()
	clk := &fakeClock{wall: base}
	clk.elapsed = func(t time.Time) time.Duration { return clk.wall.Sub(t) }
	f := newFeedWithClock(clk.now, clk.since)

	const burst = 3000
	for i := 0; i < burst; i++ {
		if i > 0 && i%1000 == 0 {
			clk.wall = clk.wall.Add(rateBucketSpan) // cross a bucket boundary
		}
		f.record(TapEvent{Type: "search"})
	}

	if got := f.Rates()["search"]; got != burst {
		t.Errorf("search rate = %d, want %d (the window counts, it does not truncate)", got, burst)
	}
	maxBuckets := int(feedRateWindow/rateBucketSpan) + 1
	if got := len(f.rates.recent["search"]); got > maxBuckets {
		t.Errorf("buckets held = %d, want <= %d (the window must stay constant-memory)", got, maxBuckets)
	}
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

// TestFeed_DroppedReflectsTapOverflow pins #1001: with the feed loop stalled, a
// burst past the tap's channel capacity must surface its drop count through
// the Feed, since Feed is the only handle the admin handler holds.
func TestFeed_DroppedReflectsTapOverflow(t *testing.T) {
	tp := New(events.NewInProcessBus())
	f := NewFeed()
	ctx, stop := context.WithCancel(context.Background())
	defer func() {
		stop()
		f.Shutdown(context.Background())
	}()

	// Hold the broadcaster lock so the loop blocks on its first event and
	// stops draining the tap channel.
	f.broadcaster.mu.Lock()
	f.Start(ctx, tp)

	const burst = tapChanSize + 100
	user := shared.NewUserId(uuid.New())
	for i := 0; i < burst; i++ {
		tp.Publish(context.Background(), user, "burst", nil)
	}
	got := f.Dropped()
	f.broadcaster.mu.Unlock()

	// The loop can have taken at most one event off the channel before
	// stalling, so at least burst-cap-1 publishes found it full.
	if floor := uint64(burst - tapChanSize - 1); got < floor {
		t.Errorf("Feed.Dropped() = %d, want >= %d", got, floor)
	}
	if got != tp.Dropped() {
		t.Errorf("Feed.Dropped() = %d, tap.Dropped() = %d, want equal", got, tp.Dropped())
	}
}

// TestFeed_AvailableOnlyWhileDraining pins #2005: a feed whose Start could not
// subscribe drains nothing, and must not report the same state as a live feed
// with no events yet.
func TestFeed_AvailableOnlyWhileDraining(t *testing.T) {
	t.Run("never started", func(t *testing.T) {
		if NewFeed().Available() {
			t.Error("Available() on an unstarted feed = true, want false")
		}
	})

	t.Run("start subscribed", func(t *testing.T) {
		f := NewFeed()
		ctx, stop := context.WithCancel(context.Background())
		defer func() {
			stop()
			f.Shutdown(context.Background())
		}()

		f.Start(ctx, New(events.NewInProcessBus()))

		if !f.Available() {
			t.Error("Available() after a successful Start = false, want true")
		}
	})

	t.Run("tap already has a subscriber", func(t *testing.T) {
		tp := New(events.NewInProcessBus())
		_, releaseTap, err := tp.SubscribeAll()
		if err != nil {
			t.Fatalf("occupy the tap: %v", err)
		}
		defer releaseTap()
		f := NewFeed()

		f.Start(context.Background(), tp)

		if f.Available() {
			t.Error("Available() after Start could not subscribe = true, want false")
		}
	})
}

func TestFeed_DroppedZeroBeforeStart(t *testing.T) {
	if got := NewFeed().Dropped(); got != 0 {
		t.Errorf("Dropped() on an unstarted feed = %d, want 0", got)
	}
}
