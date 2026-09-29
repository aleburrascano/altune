package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAllFlagsCommentsAndNamesFileLine(t *testing.T) {
	cases := map[string]struct {
		name string
		body string
		want string
	}{
		"line comment":  {"hello.go", "package p\n\n// hello\nvar x = 1\n", ":3"},
		"test file":     {"hello_test.go", "package p\n\n// hello\n", ":3"},
		"block comment": {"block.go", "package p\n\n/* x */\nvar x = 1\n", ":3"},
		"build tag":     {"tag.go", "//go:build integration\n\npackage p\n", ":1"},
		"suppression":   {"lint.go", "package p\n\nvar x = 1 //nolint:errcheck\n", ":3"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, tc.name, tc.body)

			var stdout strings.Builder
			code := run([]string{"--all", dir}, &stdout)

			if code != 1 {
				t.Fatalf("exit = %d, want 1; output:\n%s", code, stdout.String())
			}
			if !strings.Contains(stdout.String(), filepath.Join(dir, tc.name)+tc.want) {
				t.Fatalf("output missing %q:\n%s", tc.want, stdout.String())
			}
			if !strings.Contains(stdout.String(), "comment violations: 1") {
				t.Fatalf("output missing count:\n%s", stdout.String())
			}
		})
	}
}

func TestRunAllAllowsGoEmbedOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "embed.go", "package p\n\nimport _ \"embed\"\n\n//go:embed clip.mp3\nvar clip []byte\n")

	var stdout strings.Builder
	code := run([]string{"--all", dir}, &stdout)

	if code != 0 {
		t.Fatalf("exit = %d, want 0; output:\n%s", code, stdout.String())
	}
}

func TestRunAllExitsTwoOnUnparsableFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "broken.go", "package p\n\nfunc (\n")

	var stdout strings.Builder
	code := run([]string{"--all", dir}, &stdout)

	if code != 2 {
		t.Fatalf("exit = %d, want 2; output:\n%s", code, stdout.String())
	}
}

func TestRunAllSkipsVendorTestdataAndNodeModules(t *testing.T) {
	dir := t.TempDir()
	for _, skipped := range []string{"vendor", "testdata", "node_modules"} {
		writeFile(t, filepath.Join(dir, skipped), "a.go", "package p\n\n// hidden\n")
	}

	var stdout strings.Builder
	code := run([]string{"--all", dir}, &stdout)

	if code != 0 {
		t.Fatalf("exit = %d, want 0; output:\n%s", code, stdout.String())
	}
}

func TestRunAllSortsByPathThenLine(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "b.go", "package p\n\n// one\n// two\n")
	writeFile(t, dir, "a.go", "package p\n\n// one\n")

	var stdout strings.Builder
	run([]string{"--all", dir}, &stdout)

	want := []string{
		filepath.Join(dir, "a.go") + ":3",
		filepath.Join(dir, "b.go") + ":3",
		filepath.Join(dir, "b.go") + ":4",
		"comment violations: 3",
	}
	if got := strings.Split(strings.TrimSpace(stdout.String()), "\n"); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("output:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestRunAllWithoutDirExitsTwo(t *testing.T) {
	var stdout strings.Builder

	if code := run([]string{"--all"}, &stdout); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestRunDiffFlagsOnlyLinesAddedSinceBase(t *testing.T) {
	dir := t.TempDir()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.email", "test@example.com")
	gitCmd(t, dir, "config", "user.name", "test")
	writeFile(t, dir, "a.go", "package p\n\n// base\nvar x = 1\n")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "base")
	base := gitOutput(t, dir, "rev-parse", "HEAD")
	writeFile(t, dir, "a.go", "package p\n\n// base\nvar x = 1\n\n// added\nvar y = 2\n")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "second")
	t.Chdir(dir)

	var stdout strings.Builder
	code := run([]string{strings.TrimSpace(base)}, &stdout)

	want := "Checking added lines for new comments/suppressions in:\n  a.go\n" +
		"  a.go:6  new comment on a changed line — zero-comments rule\n" +
		"new-code comment/suppression violations: 1\n"
	if code != 1 || stdout.String() != want {
		t.Fatalf("exit = %d, output:\n%s\nwant:\n%s", code, stdout.String(), want)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	gitOutput(t, dir, args...)
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
