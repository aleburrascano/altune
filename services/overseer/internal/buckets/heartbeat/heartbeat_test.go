package heartbeat_test

import (
	"altune/overseer/internal/buckets/heartbeat"
	"altune/overseer/internal/core"
	"context"
	"encoding/json"
	"testing"
)

// snapshotData unmarshals a heartbeat snapshot's Data payload.
func snapshotData(t *testing.T, snap core.Snapshot) heartbeat.Data {
	t.Helper()
	var d heartbeat.Data
	if err := json.Unmarshal(snap.Data, &d); err != nil {
		t.Fatalf("unmarshal data: %v (%s)", err, snap.Data)
	}
	return d
}

// The heartbeat bucket proves the full plugin path in the small: collect ->
// store -> snapshot, with a payload that reflects the stored ticks.
func TestHeartbeatCollectStoreSnapshot(t *testing.T) {
	b := heartbeat.New()

	// Empty state is a live but empty payload, not an error.
	empty := b.Snapshot()
	if empty.State != core.StateLive {
		t.Errorf("empty state = %q, want live", empty.State)
	}
	if got := len(snapshotData(t, empty).Ticks); got != 0 {
		t.Errorf("empty ticks = %d, want 0", got)
	}

	for i := 0; i < 3; i++ {
		signals, err := b.Collect(context.Background())
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		b.Store(signals)
	}

	if got := len(snapshotData(t, b.Snapshot()).Ticks); got != 3 {
		t.Errorf("ticks = %d, want 3", got)
	}
	if b.Meta().ID != "heartbeat" {
		t.Errorf("Meta.ID = %q, want heartbeat", b.Meta().ID)
	}
}

// The heartbeat grades ok by construction — a tick from Overseer's own clock
// cannot report a fault — but its headline still tracks the payload: how much of
// the collect path it has proven so far.
func TestHeartbeatHeadlineTracksTheTicks(t *testing.T) {
	b := heartbeat.New()

	if snap := b.Snapshot(); snap.Severity != core.SeverityOK || snap.Headline != "no ticks yet" {
		t.Errorf("empty grade = %q/%q, want ok/no ticks yet", snap.Severity, snap.Headline)
	}

	for i := 0; i < 3; i++ {
		signals, err := b.Collect(context.Background())
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		b.Store(signals)
	}

	snap := b.Snapshot()
	if snap.Severity != core.SeverityOK {
		t.Errorf("severity = %q, want ok", snap.Severity)
	}
	if snap.Headline != "3 ticks" {
		t.Errorf("headline = %q, want 3 ticks", snap.Headline)
	}
}

// Bounded storage holds at the bucket level too: many collects never grow the
// snapshot's tick list past the bucket's capacity.
func TestHeartbeatStaysBounded(t *testing.T) {
	b := heartbeat.New()
	for i := 0; i < 500; i++ {
		signals, _ := b.Collect(context.Background())
		b.Store(signals)
	}
	if got := len(snapshotData(t, b.Snapshot()).Ticks); got != 60 {
		t.Errorf("ticks = %d, want cap 60", got)
	}
}
