package eventtap

import (
	"testing"

	"github.com/google/uuid"

	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
)

func TestSubscribeAll_CapturesNeverSeenUser(t *testing.T) {
	tp := New(events.NewInProcessBus())
	tap, cancel, err := tp.SubscribeAll()
	if err != nil {
		t.Fatalf("SubscribeAll: %v", err)
	}
	defer cancel()

	// A user with no prior state (lazy init) must still appear on the tap.
	freshUser := shared.NewUserId(uuid.New())
	tp.Publish(freshUser, "track_added", map[string]any{"track_id": "secret"})

	select {
	case evt := <-tap:
		if evt.Type != "track_added" {
			t.Errorf("type = %q, want track_added", evt.Type)
		}
		if evt.Timestamp.IsZero() {
			t.Error("tap event missing timestamp")
		}
	default:
		t.Fatal("event for a never-seen user did not reach the tap")
	}
}

func TestSubscribeAll_SingleConsumer(t *testing.T) {
	tp := New(events.NewInProcessBus())
	_, cancel, err := tp.SubscribeAll()
	if err != nil {
		t.Fatalf("first SubscribeAll: %v", err)
	}
	defer cancel()

	if _, _, err := tp.SubscribeAll(); err == nil {
		t.Fatal("second SubscribeAll should error (single consumer)")
	}
}

func TestSubscribeAll_SlowConsumerDropsNotBlocks(t *testing.T) {
	tp := New(events.NewInProcessBus())
	_, cancel, err := tp.SubscribeAll() // never drained
	if err != nil {
		t.Fatalf("SubscribeAll: %v", err)
	}
	defer cancel()

	user := shared.NewUserId(uuid.New())
	for i := 0; i < tapChanSize*3; i++ {
		tp.Publish(user, "spam", nil) // must not block despite a full tap
	}
	if tp.Dropped() == 0 {
		t.Error("expected some tap drops once the consumer buffer filled")
	}
}

func TestTap_ReleasedAfterCancel(t *testing.T) {
	tp := New(events.NewInProcessBus())
	_, cancel, err := tp.SubscribeAll()
	if err != nil {
		t.Fatalf("SubscribeAll: %v", err)
	}
	cancel()
	// After cancel a new consumer may subscribe.
	_, cancel2, err := tp.SubscribeAll()
	if err != nil {
		t.Fatalf("re-subscribe after cancel: %v", err)
	}
	cancel2()
}
