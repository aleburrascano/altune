// Package guard holds architectural invariant tests that assert Overseer's build
// graph, not its runtime behaviour.
package guard_test

import (
	"altune/overseer/internal/core"
	"os/exec"
	"strings"
	"testing"

	// Import a real bucket so its self-registration runs, proving the additive
	// path end to end from this out-of-tree package.
	_ "altune/overseer/internal/buckets/heartbeat"
)

// deps returns the full transitive dependency list of the given package.
func deps(t *testing.T, pkg string) []string {
	t.Helper()
	out, err := exec.Command("go", "list", "-deps", pkg).Output()
	if err != nil {
		t.Fatalf("go list -deps %s: %v", pkg, err)
	}
	return strings.Fields(string(out))
}

// Spine invariant: no internal imports. Overseer must import zero go-api internal
// packages; all app data crosses go-api's public HTTP surface only.
func TestNoGoAPIInternalImports(t *testing.T) {
	// Absolute module pattern, not "./...": `go test` runs this from the guard
	// package dir, where "./..." resolves to the guard package alone and the scan
	// passes vacuously. "altune/overseer/..." scans every package in the module.
	for _, dep := range deps(t, "altune/overseer/...") {
		if strings.HasPrefix(dep, "altune/go-api/internal") || strings.Contains(dep, "go-api/internal/") {
			t.Errorf("forbidden import of go-api internal package: %s", dep)
		}
	}
}

// Spine invariant: additive buckets. The shell core and composition path must
// reference no concrete bucket package, so a new bucket cannot force a core edit.
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

// Additive proof at runtime: the real bucket self-registered into the Default
// registry from its package init, via nothing but a blank import line.
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
