package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestCompare(t *testing.T) {
	extra := "G1 internal/x/doc.go unexpected root entry"
	cases := []struct {
		name     string
		found    []string
		recorded []string
		code     int
		want     string
	}{
		{"equal", []string{extra}, []string{extra}, 0, "module-shape: 1 violations, all in baseline\n"},
		{"extra line", []string{extra}, nil, 1, "new: " + extra + "\n"},
		{"stale line", nil, []string{extra}, 1, "fixed: remove it from the baseline: " + extra + "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if code := compare(&out, tc.found, tc.recorded); code != tc.code {
				t.Fatalf("exit %d, want %d", code, tc.code)
			}
			if out.String() != tc.want {
				t.Fatalf("got %q, want %q", out.String(), tc.want)
			}
		})
	}
}

func TestBaselineRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "module-shape.baseline")
	if err := writeBaseline(path, []string{"b", "a", "b"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "a\nb\n" {
		t.Fatalf("got %q", raw)
	}
	lines, err := readBaseline(path)
	if err != nil || !slices.Equal(lines, []string{"a", "b"}) {
		t.Fatalf("got %q, %v", lines, err)
	}
}

func TestRunWriteThenCheck(t *testing.T) {
	root := writeTree(t, map[string]string{"common.go": goSource()})
	baseline := filepath.Join(t.TempDir(), "baseline")
	var out, errOut bytes.Buffer
	if code := run([]string{"-root", root, "-baseline", baseline, "-write"}, &out, &errOut); code != 0 {
		t.Fatalf("write exit %d: %s", code, errOut.String())
	}
	if code := run([]string{"-root", root, "-baseline", baseline}, &out, &errOut); code != 0 {
		t.Fatalf("check exit %d: %s", code, out.String())
	}
	if out.String() != "module-shape: 1 violations, all in baseline\n" {
		t.Fatalf("got %q", out.String())
	}
	if err := os.Remove(filepath.Join(root, "common.go")); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"-root", root, "-baseline", baseline}, &out, &errOut); code != 1 {
		t.Fatalf("stale exit %d", code)
	}
}

func TestCollectSeesAddedFile(t *testing.T) {
	root := writeTree(t, fullModule("m", nil))
	before, err := collect(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal/m/doc.go"), []byte(goSource()), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := collect(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before)+1 {
		t.Fatalf("before %d, after %d", len(before), len(after))
	}
}
