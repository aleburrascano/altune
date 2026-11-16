package binpath_test

import (
	"altune/go-api/internal/shared/binpath"
	"os"
	"path/filepath"
	"testing"
)

func TestResolve_FallsBackToName(t *testing.T) {
	if got := binpath.Resolve("fpcalc", ""); got != "fpcalc" {
		t.Errorf("Resolve = %q, want the bare name for PATH lookup", got)
	}
	if got := binpath.Resolve("fpcalc", t.TempDir()); got != "fpcalc" {
		t.Errorf("Resolve = %q, want the bare name when the dir has no binary", got)
	}
}

func TestResolve_PrefersTheBinaryInDir(t *testing.T) {
	dir := t.TempDir()
	for _, file := range []string{"fpcalc", "fpcalc.exe"} {
		if err := os.WriteFile(filepath.Join(dir, file), []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := binpath.Resolve("fpcalc", dir)
	if got != filepath.Join(dir, "fpcalc") && got != filepath.Join(dir, "fpcalc.exe") {
		t.Errorf("Resolve = %q, want the binary in %q", got, dir)
	}
}

func TestRunnable_BareNameIsLookedUpOnPATH(t *testing.T) {
	dir := t.TempDir()
	for _, file := range []string{"fpcalc", "fpcalc.exe"} {
		if err := os.WriteFile(filepath.Join(dir, file), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	if !binpath.Runnable("fpcalc") {
		t.Error("Runnable(fpcalc) = false, want true for a name on PATH")
	}
	if binpath.Runnable("nope") {
		t.Error("Runnable(nope) = true, want false for a name not on PATH")
	}
}

func TestRunnable_AbsolutePathChecksExistence(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope")
	if binpath.Runnable(missing) {
		t.Errorf("Runnable(%q) = true, want false for a missing absolute path", missing)
	}
	present := filepath.Join(dir, "present")
	if err := os.WriteFile(present, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !binpath.Runnable(present) {
		t.Errorf("Runnable(%q) = false, want true for an existing absolute path", present)
	}
}
