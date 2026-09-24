package guard_test

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"testing"

	_ "altune/overseer/internal/buckets/backendperf"
	_ "altune/overseer/internal/buckets/cost"
	_ "altune/overseer/internal/buckets/domainquality"
	_ "altune/overseer/internal/buckets/heartbeat"
	_ "altune/overseer/internal/buckets/liveactivity"
	_ "altune/overseer/internal/buckets/logs"
	_ "altune/overseer/internal/buckets/reliability"
	_ "altune/overseer/internal/buckets/security"
	_ "altune/overseer/internal/buckets/usage"
)

func TestSourceDownSnapshotsCarryAClassifiedReason(t *testing.T) {
	valid := map[string]bool{
		goapi.ReasonAuth:      true,
		goapi.ReasonThrottled: true,
		goapi.ReasonDegraded:  true,
		goapi.ReasonDown:      true,
	}
	for _, b := range core.Default.Buckets() {
		snap := b.Snapshot()
		if snap.State != core.StateSourceDown {
			continue
		}
		if !valid[snap.Reason] {
			t.Errorf("bucket %q is source_down with reason %q, want one of auth/throttled/degraded/down", b.Meta().ID, snap.Reason)
		}
	}
}
