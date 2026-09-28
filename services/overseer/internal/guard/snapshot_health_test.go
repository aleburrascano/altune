package guard_test

import (
	"altune/overseer/internal/core"
	"strings"
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

func TestEveryBucketGradesItsOwnSnapshot(t *testing.T) {
	graded := map[core.Severity]bool{
		core.SeverityOK:       true,
		core.SeverityWarn:     true,
		core.SeverityCritical: true,
	}
	for _, b := range core.Default.Buckets() {
		id := b.Meta().ID
		snap := b.Snapshot()
		if !graded[snap.Severity] {
			t.Errorf("bucket %q severity = %q, want one of ok/warn/critical", id, snap.Severity)
		}
		if strings.TrimSpace(snap.Headline) == "" {
			t.Errorf("bucket %q headline is empty; every bucket must name the one figure that matters for it", id)
		}
	}
}

func TestEveryBucketPackageIsRepresentedInTheRegistry(t *testing.T) {
	packages := bucketPackages(t)
	registered := core.Default.Buckets()
	if len(registered) != len(packages) {
		t.Errorf("registry holds %d buckets but %d bucket packages exist (%v); add the missing blank import to this file",
			len(registered), len(packages), packages)
	}
}
