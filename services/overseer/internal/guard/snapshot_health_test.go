package guard_test

import (
	"altune/overseer/internal/app"
	"altune/overseer/internal/core"
	"strings"
	"testing"
)

func TestEveryBucketGradesItsOwnSnapshot(t *testing.T) {
	graded := map[core.Severity]bool{
		core.SeverityOK:       true,
		core.SeverityWarn:     true,
		core.SeverityCritical: true,
	}
	for _, b := range app.Buckets() {
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
	registered := app.Buckets()
	if len(registered) != len(packages) {
		t.Errorf("app.Buckets builds %d buckets but %d bucket packages exist (%v); build the missing bucket in app.Buckets",
			len(registered), len(packages), packages)
	}
}
