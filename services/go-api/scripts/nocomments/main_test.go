package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCheckExitsOneAndNamesFileLine(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "hi.sh", "#!/bin/sh\n# hi\necho hi\n")

	var stdout strings.Builder
	code := run([]string{"check", dir}, &stdout)

	if code != 1 {
		t.Fatalf("exit = %d, want 1; output:\n%s", code, stdout.String())
	}
	wantLine := filepath.Join(dir, "hi.sh") + ":2"
	if !strings.Contains(stdout.String(), wantLine) {
		t.Fatalf("output missing %q:\n%s", wantLine, stdout.String())
	}
}

func TestRunCheckExitsZeroOnShebangOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "clean.sh", "#!/bin/sh\necho hi\n")

	var stdout strings.Builder
	code := run([]string{"check", dir}, &stdout)

	if code != 0 {
		t.Fatalf("exit = %d, want 0; output:\n%s", code, stdout.String())
	}
}

func TestRunDiffFlagsOnlyLinesAddedSinceBase(t *testing.T) {
	dir := t.TempDir()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.email", "test@example.com")
	gitCmd(t, dir, "config", "user.name", "test")

	writeFile(t, dir, "a.sh", "#!/bin/sh\n# base comment\necho hi\n")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "base")
	base := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))

	writeFile(t, dir, "a.sh", "#!/bin/sh\n# base comment\necho hi\n# x\necho bye\n")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "second")

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatalf("chdir back: %v", err)
		}
	}()

	var stdout strings.Builder
	code := run([]string{"diff", base}, &stdout)

	if code != 1 {
		t.Fatalf("exit = %d, want 1; output:\n%s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "a.sh:4") {
		t.Fatalf("output missing the added comment a.sh:4:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "a.sh:2") {
		t.Fatalf("output flagged the pre-existing base comment a.sh:2:\n%s", stdout.String())
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return string(out)
}
