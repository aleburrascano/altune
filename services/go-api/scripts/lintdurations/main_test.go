package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func lint(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr strings.Builder
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestInlineLiteralsAreFlaggedWithFileAndLine(t *testing.T) {
	cases := map[string]string{
		"number first":         "package p\n\nimport \"time\"\n\nvar d = 5 * time.Second\n",
		"unit first":           "package p\n\nimport \"time\"\n\nvar d = time.Minute * 2\n",
		"float":                "package p\n\nimport \"time\"\n\nvar d = 1.5 * time.Second\n",
		"inside a function":    "package p\n\nimport \"time\"\n\nfunc f() time.Duration {\n\treturn 30 * time.Millisecond\n}\n",
		"converted number":     "package p\n\nimport \"time\"\n\nvar d = time.Duration(3) * time.Hour\n",
		"parenthesised number": "package p\n\nimport \"time\"\n\nvar d = (4) * time.Second\n",
		"days":                 "package p\n\nimport \"time\"\n\nfunc g(time.Duration) {}\n\nfunc f() { g(30 * 24 * time.Hour) }\n",
		"negative":             "package p\n\nimport \"time\"\n\nvar d = -5 * time.Second\n",
		"sum in parentheses":   "package p\n\nimport \"time\"\n\nvar d = (1 + 2) * time.Second\n",
		"arithmetic converted": "package p\n\nimport \"time\"\n\nvar d = time.Duration(2*3) * time.Second\n",
		"typed literal":        "package p\n\nimport \"time\"\n\nvar d = time.Duration(int64(5)) * time.Second\n",
		"aliased import":       "package p\n\nimport t \"time\"\n\nvar d = 5 * t.Second\n",
		"in a call argument":   "package p\n\nimport \"time\"\n\nfunc g(time.Duration) {}\n\nfunc f() { g(10 * time.Second) }\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "a.go", body)

			code, out, _ := lint(t, dir)

			if code != 1 {
				t.Fatalf("exit = %d, want 1; output:\n%s", code, out)
			}
			if !strings.Contains(out, filepath.Join(dir, "a.go")+":") {
				t.Fatalf("output does not name the file:\n%s", out)
			}
			if !strings.Contains(out, "inline duration literals: 1") {
				t.Fatalf("output missing the count:\n%s", out)
			}
		})
	}
}

func TestAcceptedFormsPass(t *testing.T) {
	cases := map[string]string{
		"named const":              "package p\n\nimport \"time\"\n\nconst timeout = 5 * time.Second\n",
		"const block":              "package p\n\nimport \"time\"\n\nconst (\n\ta = 5 * time.Second\n\tb = 2 * time.Minute\n)\n",
		"function const":           "package p\n\nimport \"time\"\n\nfunc f() time.Duration {\n\tconst wait = 10 * time.Second\n\treturn wait\n}\n",
		"variable times unit":      "package p\n\nimport \"time\"\n\nfunc f(n int) time.Duration { return time.Duration(n) * time.Second }\n",
		"unit alone":               "package p\n\nimport \"time\"\n\nvar d = time.Second\n",
		"number times a number":    "package p\n\nvar n = 5 * 3\n",
		"another package's Second": "package p\n\nimport \"x/clock\"\n\nvar d = 5 * clock.Second\n",
		"local named time":         "package p\n\nvar time = struct{ Second int }{1}\n\nvar d = 5 * time.Second\n",
		"variable arithmetic":      "package p\n\nimport \"time\"\n\nfunc f(n int) time.Duration { return time.Duration(n*24) * time.Hour }\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "a.go", body)

			code, out, _ := lint(t, dir)

			if code != 0 {
				t.Fatalf("exit = %d, want 0; output:\n%s", code, out)
			}
		})
	}
}

func TestTestFilesAndVendoredDirsAreSkipped(t *testing.T) {
	dir := t.TempDir()
	body := "package p\n\nimport \"time\"\n\nvar d = 5 * time.Second\n"
	writeFile(t, dir, "a_test.go", body)
	writeFile(t, dir, "testdata/b.go", body)
	writeFile(t, dir, "vendor/c.go", body)

	code, out, _ := lint(t, dir)

	if code != 0 {
		t.Fatalf("exit = %d, want 0; output:\n%s", code, out)
	}
}

func TestTestSupportPackagesAreChecked(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "acquisitiontest/contract.go", "package acquisitiontest\n\nimport \"time\"\n\nvar d = 5 * time.Second\n")

	code, out, _ := lint(t, dir)

	if code != 1 {
		t.Fatalf("exit = %d, want 1; output:\n%s", code, out)
	}
}

func TestSummaryCountsPerDirectory(t *testing.T) {
	dir := t.TempDir()
	body := "package p\n\nimport \"time\"\n\nvar a = 5 * time.Second\nvar b = 6 * time.Second\n"
	writeFile(t, dir, "one/a.go", body)
	writeFile(t, dir, "two/a.go", "package p\n\nimport \"time\"\n\nvar a = 5 * time.Second\n")

	code, out, _ := lint(t, "--summary", dir)

	if code != 1 {
		t.Fatalf("exit = %d, want 1; output:\n%s", code, out)
	}
	if !strings.Contains(out, "     2  "+filepath.Join(dir, "one")) || !strings.Contains(out, "     1  "+filepath.Join(dir, "two")) {
		t.Fatalf("summary does not count per directory:\n%s", out)
	}
	if strings.Contains(out, "a.go:") {
		t.Fatalf("summary should not list lines:\n%s", out)
	}
}

func TestUsageAndUnreadableInput(t *testing.T) {
	if code, _, stderr := lint(t); code != 2 || !strings.Contains(stderr, "usage:") {
		t.Fatalf("no arguments: exit = %d, stderr %q, want 2 and usage", code, stderr)
	}
	if code, _, _ := lint(t, "--bogus"); code != 2 {
		t.Fatalf("unknown flag: exit = %d, want 2", code)
	}
	if code, _, stderr := lint(t, filepath.Join(t.TempDir(), "missing")); code != 2 || !strings.Contains(stderr, "lintdurations:") {
		t.Fatalf("missing dir: exit = %d, stderr %q, want 2 and a message", code, stderr)
	}
	dir := t.TempDir()
	writeFile(t, dir, "broken.go", "package p\n\nfunc (\n")
	if code, _, stderr := lint(t, dir); code != 2 || !strings.Contains(stderr, "parse") {
		t.Fatalf("unparsable file: exit = %d, stderr %q, want 2 and a parse message", code, stderr)
	}
}

func TestSymlinkedRootAndFlagOrder(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "real/a.go", "package p\n\nimport \"time\"\n\nvar d = 5 * time.Second\n")
	if err := os.Symlink(filepath.Join(dir, "real"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}

	if code, out, _ := lint(t, filepath.Join(dir, "link")); code != 1 {
		t.Fatalf("symlinked root: exit = %d, want 1; output:\n%s", code, out)
	}
	if code, out, _ := lint(t, filepath.Join(dir, "real"), "--summary"); code != 1 || strings.Contains(out, "a.go:") {
		t.Fatalf("--summary after the directory: exit = %d, output:\n%s", code, out)
	}
}

func TestAnExplicitTestdataRootIsScanned(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "testdata")
	writeFile(t, dir, "a.go", "package p\n\nimport \"time\"\n\nvar d = 5 * time.Second\n")

	if code, out, _ := lint(t, dir); code != 1 {
		t.Fatalf("exit = %d, want 1; output:\n%s", code, out)
	}
}
