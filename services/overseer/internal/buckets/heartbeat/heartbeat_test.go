package heartbeat_test

import (
	"altune/overseer/internal/buckets/heartbeat"
	"altune/overseer/internal/core"
	"context"
	"encoding/json"
	"testing"
)

func snapshotData(t *testing.T, snap core.Snapshot) heartbeat.Data {
	t.Helper()
	var d heartbeat.Data
	if err := json.Unmarshal(snap.Data, &d); err != nil {
		t.Fatalf("unmarshal data: %v (%s)", err, snap.Data)
	}
	return d
}

func TestHeartbeatCollectStoreSnapshot(t *testing.T) {
	b := heartbeat.New()

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
