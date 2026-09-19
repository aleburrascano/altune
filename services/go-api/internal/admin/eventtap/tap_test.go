package eventtap

import (
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/logging"
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestSubscribeAll_CapturesNeverSeenUser(t *testing.T) {
	tp := New(events.NewInProcessBus())
	tap, cancel, err := tp.SubscribeAll()
	if err != nil {
		t.Fatalf("SubscribeAll: %v", err)
	}
	defer cancel()

	freshUser := shared.NewUserId(uuid.New())
	tp.Publish(context.Background(), freshUser, "track_added", map[string]any{"track_id": "secret"})

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

func TestPublish_StampsCorrelationIDFromContext(t *testing.T) {
	tp := New(events.NewInProcessBus())
	tap, cancel, err := tp.SubscribeAll()
	if err != nil {
		t.Fatalf("SubscribeAll: %v", err)
	}
	defer cancel()

	ctx := logging.WithCorrelationID(context.Background(), "req-abc123")
	tp.Publish(ctx, shared.NewUserId(uuid.New()), "track_added", nil)

	select {
	case evt := <-tap:
		if evt.CorrID != "req-abc123" {
			t.Errorf("corr_id = %q, want req-abc123", evt.CorrID)
		}
	default:
		t.Fatal("event did not reach the tap")
	}
}

func TestPublish_ContextWithoutCorrelationIDYieldsEmptyCorrID(t *testing.T) {
	tp := New(events.NewInProcessBus())
	tap, cancel, err := tp.SubscribeAll()
	if err != nil {
		t.Fatalf("SubscribeAll: %v", err)
	}
	defer cancel()

	tp.Publish(context.Background(), shared.NewUserId(uuid.New()), "track_added", nil)

	select {
	case evt := <-tap:
		if evt.CorrID != "" {
			t.Errorf("corr_id = %q, want empty for a context carrying none", evt.CorrID)
		}
	default:
		t.Fatal("event did not reach the tap")
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
	_, cancel, err := tp.SubscribeAll()
	if err != nil {
		t.Fatalf("SubscribeAll: %v", err)
	}
	defer cancel()

	user := shared.NewUserId(uuid.New())
	for i := 0; i < tapChanSize*3; i++ {
		tp.Publish(context.Background(), user, "spam", nil)
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
	_, cancel2, err := tp.SubscribeAll()
	if err != nil {
		t.Fatalf("re-subscribe after cancel: %v", err)
	}
	cancel2()
}
