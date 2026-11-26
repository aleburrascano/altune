package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testConfig = `{
  "roles": {"feat": "feature", "other": "feature", "app": "app", "plat": "platform", "shared": "shared"},
  "groups": ["plat", "shared"],
  "may_import": {"feature": ["shared"], "platform": ["shared"], "shared": ["shared"], "app": ["feature", "platform", "shared"]},
  "pure_layers": ["core", "service", "domain", "ports", "shell"],
  "io_imports": ["os", "net", "net/http/...", "github.com/jackc/pgx/..."],
  "driver_imports": ["github.com/jackc/pgx/..."],
  "driver_homes": {"segments": ["adapters"], "modules": ["shared/pool"]},
  "driver_composition": {"dirs": ["cmd/...", "internal/app", "internal/app/setup"], "imports": ["github.com/jackc/pgx/v5/pgxpool"]},
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

func TestEdges(t *testing.T) {
	cases := map[string]struct {
		file, body, want string
	}{
		"a feature imports another feature": {"internal/feat/f2.go", "package feat\n\nimport _ \"example/api/internal/other\"\n", "a feature may import only shared"},
		"a feature imports a platform":      {"internal/feat/f2.go", "package feat\n\nimport _ \"example/api/internal/plat/auth\"\n", "a feature may import only shared"},
		"a feature imports app":             {"internal/feat/f2.go", "package feat\n\nimport _ \"example/api/internal/app\"\n", "a feature may import only shared"},
		"a platform imports another":        {"internal/plat/auth/a2.go", "package auth\n\nimport _ \"example/api/internal/plat/billing\"\n", "a platform may import only shared"},
		"shared imports a feature":          {"internal/shared/clock/c2.go", "package clock\n\nimport _ \"example/api/internal/feat\"\n", "shared) imports feat"},
		"a feature test imports a feature":  {"internal/feat/f_test.go", "package feat_test\n\nimport _ \"example/api/internal/other\"\n", "a feature may import only shared"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := newTree(t)
			writeFile(t, dir, "internal/app/app.go", "package app\n")
			writeFile(t, dir, "internal/plat/billing/b.go", "package billing\n")
			writeFile(t, dir, tc.file, tc.body)

			code, out, _ := lint(t, "--rules", "edge,role", dir)

			if code != 1 || !strings.Contains(out, tc.want) {
				t.Fatalf("exit = %d, want 1 naming %q:\n%s", code, tc.want, out)
			}
		})
	}
}

func TestTheDepguardDeniesAreCovered(t *testing.T) {
	cases := map[string]struct{ file, body string }{
		"shared imports a platform module": {"internal/shared/clock/c2.go", "package clock\n\nimport _ \"example/api/internal/plat/auth\"\n"},
		"shared imports app":               {"internal/shared/clock/c2.go", "package clock\n\nimport _ \"example/api/internal/app\"\n"},
		"platform imports a feature":       {"internal/plat/auth/a2.go", "package auth\n\nimport _ \"example/api/internal/feat\"\n"},
		"platform imports app":             {"internal/plat/auth/a2.go", "package auth\n\nimport _ \"example/api/internal/app\"\n"},
		"shared imports another feature":   {"internal/shared/clock/c2.go", "package clock\n\nimport _ \"example/api/internal/other\"\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := newTree(t)
			writeFile(t, dir, "internal/app/app.go", "package app\n")
			writeFile(t, dir, tc.file, tc.body)

			code, out, _ := lint(t, "--rules", "edge", dir)

			if code != 1 || !strings.Contains(out, "may import only") {
				t.Fatalf("exit = %d:\n%s", code, out)
			}
		})
	}
}

func TestGoCodeThatBelongsToNoModuleIsReported(t *testing.T) {
	for _, file := range []string{"internal/x.go", "internal/shared/x.go", "internal/plat/x.go"} {
		t.Run(file, func(t *testing.T) {
			dir := newTree(t)
			writeFile(t, dir, file, "package x\n\nimport _ \"example/api/internal/feat\"\n")

			code, out, _ := lint(t, "--rules", "role", dir)

			if code != 1 || !strings.Contains(out, "belongs to no module") {
				t.Fatalf("exit = %d:\n%s", code, out)
			}
		})
	}
}

func TestARoleWithoutAMayImportEntryIsAConfigError(t *testing.T) {
	dir := newTree(t)
	writeFile(t, dir, "architecture.json", strings.Replace(testConfig, `"app": ["feature", "platform", "shared"]`, `"extra": []`, 1))

	code, _, stderr := lint(t, dir)

	if code != 2 || !strings.Contains(stderr, "no entry in may_import") {
		t.Fatalf("exit = %d, stderr %q", code, stderr)
	}
}

func TestAModuleImportingOneWithoutARoleIsReported(t *testing.T) {
	dir := newTree(t)
	writeFile(t, dir, "internal/feat/f2.go", "package feat\n\nimport _ \"example/api/internal/mystery\"\n")
	writeFile(t, dir, "internal/mystery/m.go", "package mystery\n")

	code, out, _ := lint(t, "--rules", "role", dir)

	if code != 1 || !strings.Contains(out, "module mystery has no role") {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
}

func TestAllowedEdgesPass(t *testing.T) {
	dir := newTree(t)
	writeFile(t, dir, "internal/app/app.go", "package app\n\nimport (\n\t_ \"example/api/internal/feat\"\n\t_ \"example/api/internal/other\"\n\t_ \"example/api/internal/plat/auth\"\n\t_ \"example/api/internal/shared/clock\"\n)\n")
	writeFile(t, dir, "internal/feat/f2.go", "package feat\n\nimport _ \"example/api/internal/shared/clock\"\n")
	writeFile(t, dir, "internal/feat/sub/s.go", "package sub\n\nimport _ \"example/api/internal/feat\"\n")
	writeFile(t, dir, "cmd/api/main.go", "package main\n\nimport _ \"example/api/internal/feat\"\n\nfunc main() {}\n")

	code, out, _ := lint(t, dir)

	if code != 0 {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
}

func TestANewModuleWithoutARoleIsReported(t *testing.T) {
	dir := newTree(t)
	writeFile(t, dir, "internal/newfeature/n.go", "package newfeature\n")

	code, out, _ := lint(t, dir)

	if code != 1 || !strings.Contains(out, "role: module newfeature has no role") {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
}

func TestPureLayersImportNoIO(t *testing.T) {
	cases := map[string]struct {
		file, body string
		code       int
	}{
		"core imports os":             {"internal/feat/internal/core/x/x.go", "package x\n\nimport _ \"os\"\n", 1},
		"service imports net/http":    {"internal/feat/service/s.go", "package service\n\nimport _ \"net/http\"\n", 1},
		"net/http subpackage":         {"internal/feat/service/s.go", "package service\n\nimport _ \"net/http/httptest\"\n", 1},
		"shell imports pgx":           {"internal/plat/auth/internal/shell/s.go", "package shell\n\nimport _ \"github.com/jackc/pgx/v5\"\n", 1},
		"core imports net/url":        {"internal/feat/domain/d.go", "package domain\n\nimport _ \"net/url\"\n", 0},
		"adapters import os":          {"internal/feat/internal/adapters/disk/d.go", "package disk\n\nimport _ \"os\"\n", 0},
		"a core test imports os":      {"internal/feat/internal/core/x/x_test.go", "package x_test\n\nimport _ \"os\"\n", 0},
		"os/signal is not os":         {"internal/feat/service/s.go", "package service\n\nimport _ \"os/signal\"\n", 0},
		"a service under an adapters": {"internal/feat/internal/adapters/service/s.go", "package service\n\nimport _ \"os\"\n", 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := newTree(t)
			writeFile(t, dir, tc.file, tc.body)

			code, out, _ := lint(t, "--rules", "layer", dir)

			if code != tc.code {
				t.Fatalf("exit = %d, want %d:\n%s", code, tc.code, out)
			}
		})
	}
}

func TestDriverImportsStayInAdapters(t *testing.T) {
	dir := newTree(t)
	writeFile(t, dir, "internal/feat/internal/adapters/db/d.go", "package db\n\nimport _ \"github.com/jackc/pgx/v5/pgxpool\"\n")
	writeFile(t, dir, "internal/feat/internal/adapters/db/d_test.go", "package db_test\n\nimport _ \"github.com/jackc/pgx/v5\"\n")
	writeFile(t, dir, "internal/app/w.go", "package app\n\nimport _ \"github.com/jackc/pgx/v5/pgxpool\"\n")
	writeFile(t, dir, "internal/app/setup/w.go", "package setup\n\nimport _ \"github.com/jackc/pgx/v5/pgxpool\"\n")
	writeFile(t, dir, "cmd/api/x/w.go", "package x\n\nimport _ \"github.com/jackc/pgx/v5/pgxpool\"\n")
	writeFile(t, dir, "internal/shared/pool/p.go", "package pool\n\nimport (\n\t_ \"github.com/jackc/pgx/v5\"\n\t_ \"github.com/jackc/pgx/v5/pgxpool\"\n)\n")
	writeFile(t, dir, "internal/app/jobs/j.go", "package jobs\n\nimport _ \"github.com/jackc/pgx/v5/pgxpool\"\n")
	writeFile(t, dir, "internal/app/w2.go", "package app\n\nimport _ \"github.com/jackc/pgx/v5\"\n")
	writeFile(t, dir, "internal/feat/t_test.go", "package feat_test\n\nimport _ \"github.com/jackc/pgx/v5\"\n")

	code, out, _ := lint(t, "--rules", "driver", dir)

	if code != 1 || !strings.Contains(out, "internal/app/jobs/j.go:3: driver:") || !strings.Contains(out, "internal/app/w2.go:3: driver:") || !strings.Contains(out, "architecture violations: 2") {
		t.Fatalf("only a job and a non-pool driver import in app should be flagged, exit = %d:\n%s", code, out)
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

			code, out, _ := lint(t, "--rules", "table", dir)

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

	code, out, _ := lint(t, "--rules", "table", dir)

	if code != 0 {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
}

func TestSummaryCountsPerRuleAndModule(t *testing.T) {
	dir := newTree(t)
	writeFile(t, dir, "internal/feat/f2.go", "package feat\n\nimport _ \"example/api/internal/other\"\n")
	writeFile(t, dir, "internal/feat/f3.go", "package feat\n\nimport _ \"example/api/internal/other\"\n")

	_, out, _ := lint(t, "--summary", dir)

	if !strings.Contains(out, "2  edge    feat") {
		t.Fatalf("summary missing the edge count:\n%s", out)
	}
}

func TestBadInputExitsTwo(t *testing.T) {
	if code, _, _ := lint(t, "--nope"); code != 2 {
		t.Fatalf("unknown flag: exit = %d, want 2", code)
	}
	if code, _, stderr := lint(t, "--rules", "bogus", newTree(t)); code != 2 || !strings.Contains(stderr, "unknown rule") {
		t.Fatalf("unknown rule: exit = %d, stderr %q", code, stderr)
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
