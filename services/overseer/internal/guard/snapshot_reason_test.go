package guard_test

import (
	"altune/overseer/internal/core"
	"testing"

	// Every real bucket, so its self-registration runs and the registry walked
	// below is the whole fleet rather than a sample.
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

// Contract invariant (failure classification): a bucket reporting source_down
// must say why. State alone collapses a dead credential and a genuinely
// unreachable go-api into the same opaque signal; every bucket that sets
// StateSourceDown is expected to classify the failure behind it via
// goapi.Classify, so Reason is always one of the four known values, never left
// blank on a source_down envelope.
func TestSourceDownSnapshotsCarryAClassifiedReason(t *testing.T) {
	valid := map[string]bool{"auth": true, "throttled": true, "degraded": true, "down": true}
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
