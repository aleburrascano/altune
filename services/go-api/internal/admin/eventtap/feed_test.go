package eventtap

import (
	"testing"
	"time"
)

func TestFeed_Rates(t *testing.T) {
	f := NewFeed()
	now := time.Now().UTC()

	f.record(TapEvent{Type: "search", Timestamp: now})
	f.record(TapEvent{Type: "search", Timestamp: now})
	f.record(TapEvent{Type: "track_added", Timestamp: now})
	// Stale event outside the window must not count.
	f.record(TapEvent{Type: "search", Timestamp: now.Add(-2 * time.Minute)})

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
