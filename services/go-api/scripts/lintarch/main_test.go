package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testConfig = `{
  "groups": ["plat", "shared"],
  "table_owners": {"tracks": "feat"}
}
`

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

func newTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example/api\n\ngo 1.26\n")
	writeFile(t, dir, "architecture.json", testConfig)
	writeFile(t, dir, "migrations/001.sql", "CREATE TABLE IF NOT EXISTS tracks (id int);\nCREATE TABLE plays (id int);\nCREATE TABLE old_stuff (id int);\n")
	writeFile(t, dir, "migrations/002.sql", "DROP TABLE IF EXISTS old_stuff;\n")
	writeFile(t, dir, "internal/feat/f.go", "package feat\n")
	writeFile(t, dir, "internal/other/o.go", "package other\n")
	writeFile(t, dir, "internal/shared/clock/c.go", "package clock\n")
	writeFile(t, dir, "internal/plat/auth/a.go", "package auth\n")
	return dir
}

func lint(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr strings.Builder
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestACleanTreeExitsZero(t *testing.T) {
	code, out, _ := lint(t, newTree(t))

	if code != 0 || !strings.Contains(out, "architecture violations: 0") {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
}

func TestOnlyTheOwnerWritesATable(t *testing.T) {
	cases := map[string]struct {
		file, body string
		want       string
	}{
		"another module updates":         {"internal/other/s.go", "package other\n\nconst q = `UPDATE tracks SET x = 1`\n", "module feat owns"},
		"another module inserts":         {"internal/other/s.go", "package other\n\nconst q = \"insert into tracks (id) values (1)\"\n", "module feat owns"},
		"another module deletes":         {"internal/other/s.go", "package other\n\nconst q = \"DELETE FROM tracks WHERE id = 1\"\n", "module feat owns"},
		"a table nobody owns is written": {"internal/feat/s.go", "package feat\n\nconst q = \"INSERT INTO plays (id) VALUES (1)\"\n", "no owner"},
		"a schema-qualified write":       {"internal/other/s.go", "package other\n\nconst q = `UPDATE public.tracks SET x = 1`\n", "module feat owns"},
		"a platform module writes":       {"internal/plat/auth/s.go", "package auth\n\nconst q = \"TRUNCATE TABLE tracks\"\n", "module feat owns"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := newTree(t)
			writeFile(t, dir, tc.file, tc.body)

			code, out, _ := lint(t, dir)

			if code != 1 || !strings.Contains(out, tc.want) {
				t.Fatalf("exit = %d, want 1 naming %q:\n%s", code, tc.want, out)
			}
		})
	}
}

func TestTableWritesThatAreFineAreNotFlagged(t *testing.T) {
	dir := newTree(t)
	writeFile(t, dir, "internal/feat/s.go", "package feat\n\nconst q = `UPDATE tracks SET x = 1`\n\nconst r = \"SELECT * FROM tracks\"\n\nconst p = \"update the queue when tracks change\"\n")
	writeFile(t, dir, "internal/other/s.go", "package other\n\nconst q = \"SELECT * FROM tracks WHERE id = 1\"\n")
	writeFile(t, dir, "internal/other/s_test.go", "package other_test\n\nconst q = \"INSERT INTO tracks (id) VALUES (1)\"\n")
	writeFile(t, dir, "internal/other/c.go", "package other\n\n// UPDATE tracks SET x = 1\nconst z = 1\n")
	writeFile(t, dir, "internal/other/d.go", "package other\n\nconst z = \"DELETE FROM old_stuff\"\n")

	code, out, _ := lint(t, dir)

	if code != 0 {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
}

func TestSummaryCountsPerModule(t *testing.T) {
	dir := newTree(t)
	writeFile(t, dir, "internal/other/s.go", "package other\n\nconst q = `UPDATE tracks SET x = 1`\n")
	writeFile(t, dir, "internal/other/s2.go", "package other\n\nconst q = `DELETE FROM tracks`\n")

	_, out, _ := lint(t, "--summary", dir)

	if !strings.Contains(out, "2  other") {
		t.Fatalf("summary missing the count for other:\n%s", out)
	}
}

func TestBadInputExitsTwo(t *testing.T) {
	if code, _, _ := lint(t, "--nope"); code != 2 {
		t.Fatalf("unknown flag: exit = %d, want 2", code)
	}
	if code, _, stderr := lint(t, t.TempDir()); code != 2 || !strings.Contains(stderr, "lintarch:") {
		t.Fatalf("no config: exit = %d, stderr %q", code, stderr)
	}
	dir := newTree(t)
	writeFile(t, dir, "architecture.json", `{"unknown_field": 1}`)
	if code, _, stderr := lint(t, dir); code != 2 || !strings.Contains(stderr, "architecture.json") {
		t.Fatalf("unknown config field: exit = %d, stderr %q", code, stderr)
	}
}
