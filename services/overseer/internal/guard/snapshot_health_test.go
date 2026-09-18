package guard_test

import (
	"altune/overseer/internal/core"
	"strings"
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

// Contract invariant (health model): every bucket grades its own snapshot. The
// shared overview reads severity + headline off every envelope, so a bucket that
// leaves either unset serializes `"severity": ""` — a value the frontend's union
// does not admit — and reads as silently healthy, which is the exact failure the
// health model exists to remove. The registry is walked rather than a list being
// kept here, so a bucket added later is covered the day it self-registers.
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

// The walk above only proves what it can reach, so this pins the reach: one
// registered bucket per bucket package. A failure means either a new bucket
// package is missing its blank import in this file (so the grading invariant is
// not being checked against it) or a package registers a number of buckets other
// than one.
func TestEveryBucketPackageIsRepresentedInTheRegistry(t *testing.T) {
	packages := bucketPackages(t)
	registered := core.Default.Buckets()
	if len(registered) != len(packages) {
		t.Errorf("registry holds %d buckets but %d bucket packages exist (%v); add the missing blank import to this file",
			len(registered), len(packages), packages)
	}
}
