package heartbeat_test

import (
	"altune/overseer/internal/buckets/heartbeat"
	"altune/overseer/internal/core"
	"testing"
	"time"
)

func TestRestoredTicksShowInTheSnapshotAndDateIt(t *testing.T) {
	b := heartbeat.New()
	last := time.Date(2026, 9, 1, 12, 0, 30, 0, time.UTC)
	restored := []core.Signal{{At: last.Add(-30 * time.Second), Kind: "tick", Text: "alive"}, {At: last, Kind: "tick", Text: "alive"}}

	ring := b.Rings()["ticks"]
	if ring == nil {
		t.Fatal("Rings() has no ticks ring")
	}

	ring.Restore(restored)

	snap := b.Snapshot()
	if got := snapshotData(t, snap).Ticks; len(got) != 2 || !got[1].At.Equal(last) {
		t.Fatalf("snapshot ticks = %+v, want the restored %+v", got, restored)
	}
	if !snap.UpdatedAt.Equal(last) {
		t.Fatalf("UpdatedAt = %v, want the newest restored tick %v", snap.UpdatedAt, last)
	}
}
