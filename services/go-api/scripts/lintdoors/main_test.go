package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func lint(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr strings.Builder
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func newTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example/api\n\ngo 1.26\n")
	writeFile(t, dir, "internal/lib/lib.go", "package lib\n\nconst Door = 1\n\nconst hidden = 2\n\nfunc Open() {}\n\ntype Thing struct{}\n\nfunc (Thing) Method() {}\n")
	writeFile(t, dir, "internal/lib/service/track/track.go", "package track\n")
	writeFile(t, dir, "internal/lib/internal/core/core.go", "package core\n")
	writeFile(t, dir, "internal/other/other.go", "package other\n")
	return dir
}

func TestImportOfANonRootPackageFromOutsideItsModuleIsFlagged(t *testing.T) {
	cases := map[string]struct{ name, body string }{
		"another module":      {"internal/other/o.go", "package other\n\nimport _ \"example/api/internal/lib/service/track\"\n"},
		"a test file":         {"internal/other/o_test.go", "package other\n\nimport _ \"example/api/internal/lib/service/track\"\n"},
		"the command":         {"cmd/api/main.go", "package main\n\nimport _ \"example/api/internal/lib/service/track\"\n\nfunc main() {}\n"},
		"an aliased import":   {"internal/other/o.go", "package other\n\nimport tr \"example/api/internal/lib/service/track\"\n\nvar _ = tr.X\n"},
		"a grouped import":    {"internal/other/o.go", "package other\n\nimport (\n\t\"fmt\"\n\t_ \"example/api/internal/lib/service/track\"\n)\n\nvar _ = fmt.Sprint\n"},
		"a module's internal": {"internal/other/o.go", "package other\n\nimport _ \"example/api/internal/lib/internal/core\"\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := newTree(t)
			writeFile(t, dir, tc.name, tc.body)

			code, out, _ := lint(t, dir)

			if code != 1 {
				t.Fatalf("exit = %d, want 1; output:\n%s", code, out)
			}
			if !strings.Contains(out, filepath.Join(dir, tc.name)+":") {
				t.Fatalf("output does not name the file:\n%s", out)
			}
			if !strings.Contains(out, "side door: 1") {
				t.Fatalf("output missing the count:\n%s", out)
			}
		})
	}
}

func TestImportsThroughTheDoorOrWithinTheModuleAreAllowed(t *testing.T) {
	cases := map[string]struct{ name, body string }{
		"the root package":         {"internal/other/o.go", "package other\n\nimport _ \"example/api/internal/lib\"\n"},
		"inside the same module":   {"internal/lib/service/track/t2.go", "package track\n\nimport _ \"example/api/internal/lib/internal/core\"\n"},
		"a sibling inside":         {"internal/lib/lib2.go", "package lib\n\nimport _ \"example/api/internal/lib/service/track\"\n"},
		"the standard library":     {"internal/other/o.go", "package other\n\nimport _ \"net/http\"\n"},
		"another module's package": {"internal/other/o.go", "package other\n\nimport _ \"github.com/x/y/internal/lib/service/track\"\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := newTree(t)
			writeFile(t, dir, tc.name, tc.body)

			code, out, _ := lint(t, dir)

			if code != 0 {
				t.Fatalf("exit = %d, want 0; output:\n%s", code, out)
			}
		})
	}
}

func TestSummaryListsOpenPackagesTheirImportersAndTheRootsExports(t *testing.T) {
	dir := newTree(t)
	writeFile(t, dir, "internal/other/o.go", "package other\n\nimport _ \"example/api/internal/lib/service/track\"\n")
	writeFile(t, dir, "cmd/api/main.go", "package main\n\nimport _ \"example/api/internal/lib/service/track\"\n\nfunc main() {}\n")

	code, out, _ := lint(t, "--summary", dir)

	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	for _, want := range []string{
		"lib: 1 open non-root packages, 3 exported identifiers at the root",
		"lib/service/track (0 exported identifiers)",
		"1  internal/other",
		"1  cmd/api",
		"other: 0 open non-root packages, 0 exported identifiers at the root",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("summary missing %q:\n%s", want, out)
		}
	}
}

func TestCleanTreeExitsZero(t *testing.T) {
	code, out, _ := lint(t, newTree(t))

	if code != 0 || !strings.Contains(out, "side door: 0") {
		t.Fatalf("exit = %d, output:\n%s", code, out)
	}
}

func TestBadInputExitsTwo(t *testing.T) {
	if code, _, _ := lint(t, "--nope"); code != 2 {
		t.Fatalf("unknown flag: exit = %d, want 2", code)
	}
	if code, _, stderr := lint(t, t.TempDir()); code != 2 || !strings.Contains(stderr, "lintdoors:") {
		t.Fatalf("no go.mod: exit = %d, stderr %q", code, stderr)
	}
}
