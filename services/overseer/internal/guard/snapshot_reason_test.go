package guard_test

import (
	"altune/overseer/internal/app"
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"testing"
)

func TestSourceDownSnapshotsCarryAClassifiedReason(t *testing.T) {
	valid := map[string]bool{
		goapi.ReasonAuth:      true,
		goapi.ReasonThrottled: true,
		goapi.ReasonDegraded:  true,
		goapi.ReasonDown:      true,
	}
	for _, b := range app.Buckets() {
		snap := b.Snapshot()
		if snap.State != core.StateSourceDown {
			continue
		}
		if !valid[snap.Reason] {
			t.Errorf("bucket %q is source_down with reason %q, want one of auth/throttled/degraded/down", b.Meta().ID, snap.Reason)
		}
	}
}
