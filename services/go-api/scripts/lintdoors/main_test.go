package main

import (
	"os"
	"path/filepath"
	"strconv"
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
			if !strings.Contains(out, "production: 1") {
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
		"lib: 3 exported at the root, 0 used outside by production code, 0 used outside only by tests, 3 unused outside; 1 open packages",
		"lib/service/track",
		"1  internal/other",
		"1  cmd/api",
		"other: 0 exported at the root",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("summary missing %q:\n%s", want, out)
		}
	}
}

func TestEachMemberOfAGroupingDirectoryIsItsOwnModule(t *testing.T) {
	dir := newTree(t)
	writeFile(t, dir, "internal/platform/clock/clock.go", "package clock\n\nfunc Now() {}\n")
	writeFile(t, dir, "internal/platform/clock/internal/x/x.go", "package x\n")
	writeFile(t, dir, "internal/platform/ids/ids.go", "package ids\n\nimport _ \"example/api/internal/platform/clock\"\n")
	writeFile(t, dir, "internal/other/o.go", "package other\n\nimport . \"example/api/internal/platform/clock/internal/x\"\n")

	code, out, _ := lint(t, "--summary", dir)

	if code != 1 {
		t.Fatalf("exit = %d, want 1; output:\n%s", code, out)
	}
	for _, want := range []string{"platform/clock: 1 exported at the root", "platform/ids: 0 exported at the root", "platform/clock/internal/x"} {
		if !strings.Contains(out, want) {
			t.Fatalf("summary missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "platform: ") {
		t.Fatalf("the grouping directory itself is not a module:\n%s", out)
	}
}

func TestTheTestDoorIsOnlyForTestFiles(t *testing.T) {
	cases := map[string]struct {
		name, body string
		want       int
	}{
		"another module's test file":     {"internal/other/o_test.go", "package other\n\nimport _ \"example/api/internal/lib/libtest\"\n", 0},
		"another module's production":    {"internal/other/o.go", "package other\n\nimport _ \"example/api/internal/lib/libtest\"\n", 1},
		"its own production code":        {"internal/lib/l2.go", "package lib\n\nimport _ \"example/api/internal/lib/libtest\"\n", 1},
		"its own test file":              {"internal/lib/l2_test.go", "package lib\n\nimport _ \"example/api/internal/lib/libtest\"\n", 0},
		"a test-named module in a test":  {"internal/other/o_test.go", "package other\n\nimport _ \"example/api/internal/shared/testdb\"\n", 0},
		"a test-named module in product": {"internal/other/o.go", "package other\n\nimport _ \"example/api/internal/shared/testdb\"\n", 1},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := newTree(t)
			writeFile(t, dir, "internal/lib/libtest/libtest.go", "package libtest\n")
			writeFile(t, dir, "internal/shared/testdb/testdb.go", "package testdb\n")
			writeFile(t, dir, tc.name, tc.body)

			code, out, _ := lint(t, dir)

			if (code == 1) != (tc.want == 1) || !strings.Contains(out, "production: "+strconv.Itoa(tc.want)) {
				t.Fatalf("exit = %d, want %d violations; output:\n%s", code, tc.want, out)
			}
		})
	}
}

func TestSummaryCountsRootIdentifiersUsedOutsideTheModule(t *testing.T) {
	dir := newTree(t)
	writeFile(t, dir, "internal/other/o.go", "package other\n\nimport \"example/api/internal/lib\"\n\nvar _ = lib.Open\n")
	writeFile(t, dir, "internal/other/o_test.go", "package other\n\nimport l \"example/api/internal/lib\"\n\nvar _ = l.Thing{}\n")
	writeFile(t, dir, "internal/lib/lib2.go", "package lib\n\nvar _ = Door\n")

	code, out, _ := lint(t, "--summary", dir)

	want := "lib: 3 exported at the root, 1 used outside by production code, 1 used outside only by tests, 1 unused outside"
	if code != 0 || !strings.Contains(out, want) {
		t.Fatalf("exit = %d, missing %q:\n%s", code, want, out)
	}
}

func TestCleanTreeExitsZero(t *testing.T) {
	code, out, _ := lint(t, newTree(t))

	if code != 0 || !strings.Contains(out, "production: 0") {
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
