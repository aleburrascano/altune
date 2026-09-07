package binpath

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolve_FallsBackToName(t *testing.T) {
	if got := Resolve("fpcalc", ""); got != "fpcalc" {
		t.Errorf("Resolve = %q, want the bare name for PATH lookup", got)
	}
	if got := Resolve("fpcalc", t.TempDir()); got != "fpcalc" {
		t.Errorf("Resolve = %q, want the bare name when the dir has no binary", got)
	}
}

func TestRunnable_AbsolutePathChecksExistence(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope")
	if Runnable(missing) {
		t.Errorf("Runnable(%q) = true, want false for a missing absolute path", missing)
	}
	present := filepath.Join(dir, "present")
	if err := os.WriteFile(present, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !Runnable(present) {
		t.Errorf("Runnable(%q) = false, want true for an existing absolute path", present)
	}
}
