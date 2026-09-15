package heartbeat_test

import (
	"altune/overseer/internal/buckets/heartbeat"
	"context"
	"strings"
	"testing"
)

// The heartbeat bucket proves the full plugin path in the small: collect ->
// store -> render, with a panel that reflects the stored ticks.
func TestHeartbeatCollectStoreRender(t *testing.T) {
	b := heartbeat.New()

	// Empty state renders a visible gap, not an error.
	if body := string(b.Render().Body); !strings.Contains(body, "no ticks") {
		t.Errorf("empty render = %q, want a no-ticks gap", body)
	}

	for i := 0; i < 3; i++ {
		signals, err := b.Collect(context.Background())
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		b.Store(signals)
	}

	body := string(b.Render().Body)
	if !strings.Contains(body, "3 tick(s) stored") {
		t.Errorf("render = %q, want 3 ticks", body)
	}
	if b.Meta().ID != "heartbeat" {
		t.Errorf("Meta.ID = %q, want heartbeat", b.Meta().ID)
	}
}

// Bounded storage holds at the bucket level too: many collects never grow the
// rendered list past the bucket's capacity.
func TestHeartbeatStaysBounded(t *testing.T) {
	b := heartbeat.New()
	for i := 0; i < 500; i++ {
		signals, _ := b.Collect(context.Background())
		b.Store(signals)
	}
	body := string(b.Render().Body)
	if !strings.Contains(body, "60 tick(s) stored") {
		t.Errorf("render did not cap at capacity 60: %q", body)
	}
}
