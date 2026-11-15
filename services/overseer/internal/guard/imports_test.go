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

func TestAppImportsNoConcreteBucket(t *testing.T) {
	for _, dep := range deps(t, "altune/overseer/internal/app") {
		if strings.Contains(dep, "altune/overseer/internal/buckets/") {
			t.Errorf("internal/app must not depend on a concrete bucket, but imports %s", dep)
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
