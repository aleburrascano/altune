package eventtap

import (
	"altune/go-api/internal/shared/events"
	"testing"
)

// TestTap_EmitReachesSubscriberWithoutTouchingInnerPublisher pins #2594: Emit
// must surface on the admin feed exactly like Publish does, but it must never
// forward to the wrapped inner publisher, so an admin-only activity signal
// (discovery's search/play telemetry) never lands in a mobile client's own
// per-user event stream, and never carries a user id or subject.
func TestTap_EmitReachesSubscriberWithoutTouchingInnerPublisher(t *testing.T) {
	bus := events.NewInProcessBus()
	tp := New(bus)
	ch, cancel, err := tp.SubscribeAll()
	if err != nil {
		t.Fatalf("SubscribeAll: %v", err)
	}
	defer cancel()

	before := bus.HighestIssuedID()
	tp.EmitAdminOnly("search_performed")

	select {
	case evt := <-ch:
		if evt.Type != "search_performed" {
			t.Errorf("type = %q, want search_performed", evt.Type)
		}
		if evt.User != "" {
			t.Errorf("user = %q, want empty", evt.User)
		}
		if evt.Subject != "" {
			t.Errorf("subject = %q, want empty", evt.Subject)
		}
	default:
		t.Fatal("Emit did not reach the tap subscriber")
	}

	if after := bus.HighestIssuedID(); after != before {
		t.Errorf("HighestIssuedID moved from %d to %d, Emit must never publish to the inner bus", before, after)
	}
}
