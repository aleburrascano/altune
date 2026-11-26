package eventwatch_test

import (
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/events/eventstest"
	"altune/go-api/internal/shared/ids"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

const (
	feedCeiling    = 16
	feedBufferSize = 64
)

func startFeed(t *testing.T) (*monitoring.EventFeed, *monitoring.EventTap, context.CancelFunc) {
	t.Helper()
	f := monitoring.NewEventFeed()
	tp := monitoring.NewEventTap(events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor")))
	ctx, stop := context.WithCancel(context.Background())
	f.Start(ctx, tp)
	t.Cleanup(func() {
		stop()
		f.Shutdown(context.Background())
	})
	return f, tp, stop
}

var publisher = ids.NewUserId(uuid.New())

func publish(tp *monitoring.EventTap, eventType string) {
	tp.Publish(context.Background(), publisher, eventstest.Named(events.EventType(eventType)))
}

func awaitType[T any](t *testing.T, ch <-chan T) string {
	t.Helper()
	select {
	case evt, open := <-ch:
		if !open {
			t.Fatal("subscriber channel closed instead of delivering the event")
		}
		return eventType(t, evt)
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber did not receive the event within 2s")
	}
	return ""
}

func eventType[T any](t *testing.T, evt T) string {
	t.Helper()
	raw, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("encode event: %v", err)
	}
	var fields struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("decode event %s: %v", raw, err)
	}
	return fields.Type
}

func closesWithin[T any](ch <-chan T, limit time.Duration) bool {
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

func subscribeAll(t *testing.T, f *monitoring.EventFeed, n int) []func() {
	t.Helper()
	cancels := make([]func(), 0, n)
	for i := 0; i < n; i++ {
		_, cancel, err := f.Subscribe()
		if err != nil {
			t.Fatalf("subscriber %d of %d: %v", i+1, n, err)
		}
		cancels = append(cancels, cancel)
		t.Cleanup(cancel)
	}
	return cancels
}

func TestFeed_FanOutToSubscribers(t *testing.T) {
	f, tp, _ := startFeed(t)
	ch, cancel, err := f.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	publish(tp, "live")

	select {
	case evt := <-ch:
		if evt.Type != "live" || evt.Timestamp.IsZero() {
			t.Errorf("event = %+v, want type live with a timestamp", evt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber did not receive the event")
	}
}

func TestFeed_SubscribeRejectsPastCeiling(t *testing.T) {
	f, tp, _ := startFeed(t)
	nextTypes := make([]func() string, 0, feedCeiling)
	cancels := make([]func(), 0, feedCeiling)
	for i := 0; i < feedCeiling; i++ {
		ch, cancel, err := f.Subscribe()
		if err != nil {
			t.Fatalf("subscriber %d: %v", i+1, err)
		}
		t.Cleanup(cancel)
		nextTypes = append(nextTypes, func() string { return awaitType(t, ch) })
		cancels = append(cancels, cancel)
	}

	if ch, cancel, err := f.Subscribe(); !errors.Is(err, monitoring.ErrTooManySubscribers) || ch != nil || cancel != nil {
		t.Fatalf("subscribe past ceiling = (%v, %v, %v), want ErrTooManySubscribers and no channel", ch, cancel != nil, err)
	}

	publish(tp, "still-live")
	for i, nextType := range nextTypes {
		if got := nextType(); got != "still-live" {
			t.Fatalf("subscriber %d got %q, want still-live", i+1, got)
		}
	}

	cancels[0]()
	_, cancel, err := f.Subscribe()
	if err != nil {
		t.Fatalf("subscribe after a cancel freed a slot: %v", err)
	}
	t.Cleanup(cancel)
}

func TestFeed_AvailableOnlyWhileDraining(t *testing.T) {
	t.Run("never started", func(t *testing.T) {
		if monitoring.NewEventFeed().Available() {
			t.Error("Available() on an unstarted feed = true, want false")
		}
	})

	t.Run("start subscribed", func(t *testing.T) {
		f, _, _ := startFeed(t)

		if !f.Available() {
			t.Error("Available() after a successful Start = false, want true")
		}
	})

	t.Run("tap already has a subscriber", func(t *testing.T) {
		tp := monitoring.NewEventTap(events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor")))
		_, releaseTap, err := tp.SubscribeAll()
		if err != nil {
			t.Fatalf("occupy the tap: %v", err)
		}
		defer releaseTap()
		f := monitoring.NewEventFeed()

		f.Start(context.Background(), tp)

		if f.Available() {
			t.Error("Available() after Start could not subscribe = true, want false")
		}
	})
}

func TestFeed_ClosesSubscriberChannelsWhenLoopStops(t *testing.T) {
	f, _, stop := startFeed(t)
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
	if f.Available() {
		t.Error("Available() after the feed stopped = true, want false")
	}
	if _, _, err := f.Subscribe(); !errors.Is(err, monitoring.ErrFeedStopped) {
		t.Fatalf("Subscribe after stop = %v, want ErrFeedStopped", err)
	}
}

func TestFeed_StopClosesEverySubscriberNotJustOne(t *testing.T) {
	f, _, stop := startFeed(t)
	closed := make([]func() bool, 0, feedCeiling)
	for i := 0; i < feedCeiling; i++ {
		ch, cancel, err := f.Subscribe()
		if err != nil {
			t.Fatalf("subscriber %d: %v", i+1, err)
		}
		t.Cleanup(cancel)
		closed = append(closed, func() bool { return closesWithin(ch, 2*time.Second) })
	}

	stop()
	f.Shutdown(context.Background())

	for i, closesInTime := range closed {
		if !closesInTime() {
			t.Errorf("subscriber %d of %d still open 2s after the feed stopped", i+1, feedCeiling)
		}
	}
}

func TestFeed_SlowSubscriberDoesNotStallShutdownAndStillSeesClose(t *testing.T) {
	f, tp, stop := startFeed(t)
	slow, cancel, err := f.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	t.Cleanup(cancel)
	for i := 0; i < 5000; i++ {
		publish(tp, "flood")
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
		f, tp, stop := startFeed(t)
		ch, cancel, err := f.Subscribe()
		if err != nil {
			t.Fatalf("round %d Subscribe: %v", round, err)
		}
		publishing := make(chan struct{})
		go func() {
			defer close(publishing)
			for i := 0; i < 500; i++ {
				publish(tp, "in-flight")
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
	f, _, stop := startFeed(t)
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
	f, _, stop := startFeed(t)
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
		f, _, stop := startFeed(t)
		cancels := subscribeAll(t, f, feedCeiling)
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
		closed func() bool
		cancel func()
		err    error
	}
	for round := 0; round < 50; round++ {
		f, _, stop := startFeed(t)
		results := make(chan sub, feedCeiling)
		go func() {
			defer close(results)
			for i := 0; i < feedCeiling; i++ {
				ch, cancel, err := f.Subscribe()
				results <- sub{func() bool { return closesWithin(ch, 2*time.Second) }, cancel, err}
			}
		}()

		stop()
		f.Shutdown(context.Background())

		for r := range results {
			if r.err != nil {
				if !errors.Is(r.err, monitoring.ErrFeedStopped) {
					t.Fatalf("round %d: Subscribe racing stop = %v, want nil or ErrFeedStopped", round, r.err)
				}
				continue
			}
			if !r.closed() {
				t.Fatalf("round %d: a subscriber admitted while the feed stopped was never closed", round)
			}
			r.cancel()
		}
	}
}

func TestFeed_OverflowingSubscriberRaisesDropped(t *testing.T) {
	f, tp, _ := startFeed(t)
	_, cancelStalled, err := f.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe stalled: %v", err)
	}
	defer cancelStalled()
	reader, cancelReader, err := f.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe reader: %v", err)
	}
	defer cancelReader()

	for i := 0; i < feedBufferSize+5; i++ {
		publish(tp, "flood")
		awaitType(t, reader)
	}
	_, settled, err := f.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe to settle the last broadcast: %v", err)
	}
	settled()

	if got := f.Dropped(); got != 5 {
		t.Errorf("Dropped() = %d, want 5", got)
	}
}
