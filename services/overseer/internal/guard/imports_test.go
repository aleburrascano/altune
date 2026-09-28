package guard_test

import (
	"altune/overseer/internal/core"
	"os/exec"
	"strings"
	"testing"

	_ "altune/overseer/internal/buckets/heartbeat"
)

func deps(t *testing.T, pkg string) []string {
	t.Helper()
	out, err := exec.Command("go", "list", "-deps", pkg).Output()
	if err != nil {
		t.Fatalf("go list -deps %s: %v", pkg, err)
	}
	return strings.Fields(string(out))
}

func TestNoGoAPIInternalImports(t *testing.T) {
	for _, dep := range deps(t, "altune/overseer/...") {
		if strings.HasPrefix(dep, "altune/go-api/internal") || strings.Contains(dep, "go-api/internal/") {
			t.Errorf("forbidden import of go-api internal package: %s", dep)
		}
	}
}

func TestCoreReferencesNoConcreteBucket(t *testing.T) {
	for _, pkg := range []string{
		"altune/overseer/internal/core",
		"altune/overseer/internal/shell",
		"altune/overseer/internal/app",
	} {
		for _, dep := range deps(t, pkg) {
			if strings.Contains(dep, "altune/overseer/internal/buckets/") {
				t.Errorf("%s must not depend on a concrete bucket, but imports %s", pkg, dep)
			}
		}
	}
}

func TestRealBucketsSelfRegister(t *testing.T) {
	got := map[string]bool{}
	for _, b := range core.Default.Buckets() {
		got[b.Meta().ID] = true
	}
	for _, want := range []string{"heartbeat"} {
		if !got[want] {
			t.Errorf("bucket %q did not self-register into core.Default", want)
		}
	}
}
