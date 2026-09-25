package eventtap

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
