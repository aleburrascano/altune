package eventtap

import (
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

import (
	"altune/go-api/internal/shared/events"
	"context"
	"errors"
	"testing"
	"time"
)

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

func TestFeed_ClosesSubscriberChannelsWhenLoopStops(t *testing.T) {
	f := NewFeed()
	ctx, stop := context.WithCancel(context.Background())
	f.Start(ctx, New(events.NewInProcessBus()))

	ch, cancel, err := f.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	stop()
	f.Shutdown(context.Background())

	select {
	case _, open := <-ch:
		if open {
			t.Fatal("subscriber channel delivered a value instead of closing")
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber channel did not close after the feed stopped")
	}

	if _, _, err := f.Subscribe(); !errors.Is(err, ErrFeedStopped) {
		t.Fatalf("Subscribe after stop = %v, want ErrFeedStopped", err)
	}
}

func startProbeFeed(t *testing.T) (*Feed, *Tap, context.CancelFunc) {
	t.Helper()
	f := NewFeed()
	tp := New(events.NewInProcessBus())
	ctx, stop := context.WithCancel(context.Background())
	f.Start(ctx, tp)
	t.Cleanup(func() {
		stop()
		f.Shutdown(context.Background())
	})
	return f, tp, stop
}

func closesWithin(ch <-chan TapEvent, limit time.Duration) bool {
	deadline := time.After(limit)
	for {
		select {
		case _, open := <-ch:
			if !open {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

func TestFeed_StopClosesEverySubscriberNotJustOne(t *testing.T) {
	f, _, stop := startProbeFeed(t)
	chans := make([]<-chan TapEvent, 0, MaxSubscribers)
	for i := 0; i < MaxSubscribers; i++ {
		ch, cancel, err := f.Subscribe()
		if err != nil {
			t.Fatalf("subscriber %d: %v", i+1, err)
		}
		t.Cleanup(cancel)
		chans = append(chans, ch)
	}

	stop()
	f.Shutdown(context.Background())

	for i, ch := range chans {
		if !closesWithin(ch, 2*time.Second) {
			t.Errorf("subscriber %d of %d still open 2s after the feed stopped", i+1, MaxSubscribers)
		}
	}
}

func TestFeed_SlowSubscriberDoesNotStallShutdownAndStillSeesClose(t *testing.T) {
	f, tp, stop := startProbeFeed(t)
	slow, cancel, err := f.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	t.Cleanup(cancel)
	user := shared.NewUserId(uuid.New())
	for i := 0; i < 5000; i++ {
		tp.Publish(context.Background(), user, "flood", nil)
	}

	stop()
	ctx, cancelWait := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelWait()
	f.Shutdown(ctx)
	if ctx.Err() != nil {
		t.Fatal("Shutdown spent its whole 2s budget behind a subscriber that never read")
	}

	if !closesWithin(slow, 2*time.Second) {
		t.Fatal("subscriber with a full buffer never saw its channel close")
	}
}

func TestFeed_StopWhileEventsAreInFlightNeverPanicsAndClosesSubscribers(t *testing.T) {
	for round := 0; round < 20; round++ {
		f, tp, stop := startProbeFeed(t)
		ch, cancel, err := f.Subscribe()
		if err != nil {
			t.Fatalf("round %d Subscribe: %v", round, err)
		}
		user := shared.NewUserId(uuid.New())
		publishing := make(chan struct{})
		go func() {
			defer close(publishing)
			for i := 0; i < 500; i++ {
				tp.Publish(context.Background(), user, "in-flight", nil)
			}
		}()

		stop()
		f.Shutdown(context.Background())
		<-publishing

		if !closesWithin(ch, 2*time.Second) {
			t.Fatalf("round %d: subscriber still open after the feed stopped mid-send", round)
		}
		cancel()
	}
}

func TestFeed_CancellingAfterStopAndTwiceIsHarmless(t *testing.T) {
	f, _, stop := startProbeFeed(t)
	ch, cancel, err := f.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	stop()
	f.Shutdown(context.Background())
	if !closesWithin(ch, 2*time.Second) {
		t.Fatal("subscriber still open after the feed stopped")
	}

	cancel()
	cancel()
}

func TestFeed_ShutdownTwiceIsHarmless(t *testing.T) {
	f, _, stop := startProbeFeed(t)
	ch, cancel, err := f.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	stop()
	f.Shutdown(context.Background())
	f.Shutdown(context.Background())

	if !closesWithin(ch, 2*time.Second) {
		t.Fatal("subscriber still open after a double shutdown")
	}
}

func TestFeed_CancelRacingStopNeverPanics(t *testing.T) {
	for round := 0; round < 50; round++ {
		f, _, stop := startProbeFeed(t)
		cancels := make([]func(), 0, MaxSubscribers)
		for i := 0; i < MaxSubscribers; i++ {
			_, cancel, err := f.Subscribe()
			if err != nil {
				t.Fatalf("round %d subscriber %d: %v", round, i+1, err)
			}
			cancels = append(cancels, cancel)
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			for _, c := range cancels {
				c()
			}
		}()

		stop()
		f.Shutdown(context.Background())
		<-done
	}
}

func TestFeed_SubscribeRacingStopEitherRefusesOrLaterCloses(t *testing.T) {
	type sub struct {
		ch     <-chan TapEvent
		cancel func()
		err    error
	}
	for round := 0; round < 50; round++ {
		f, _, stop := startProbeFeed(t)
		results := make(chan sub, MaxSubscribers)
		go func() {
			defer close(results)
			for i := 0; i < MaxSubscribers; i++ {
				ch, cancel, err := f.Subscribe()
				results <- sub{ch, cancel, err}
			}
		}()

		stop()
		f.Shutdown(context.Background())

		for r := range results {
			if r.err != nil {
				if !errors.Is(r.err, ErrFeedStopped) {
					t.Fatalf("round %d: Subscribe racing stop = %v, want nil or ErrFeedStopped", round, r.err)
				}
				continue
			}
			if !closesWithin(r.ch, 2*time.Second) {
				t.Fatalf("round %d: a subscriber admitted while the feed stopped was never closed", round)
			}
			r.cancel()
		}
	}
}
