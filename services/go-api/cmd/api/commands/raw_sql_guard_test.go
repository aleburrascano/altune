package commands

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var rawSQLMethods = map[string]bool{"Query": true, "QueryRow": true, "Exec": true, "Begin": true}

// rawSQLCalls returns the position of every call that selects a raw SQL method on any receiver.
func rawSQLCalls(t *testing.T, fset *token.FileSet, name string, src any) []string {
	t.Helper()
	file, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	var hits []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && rawSQLMethods[sel.Sel.Name] {
			hits = append(hits, fset.Position(call.Pos()).String()+" "+sel.Sel.Name)
		}
		return true
	})
	return hits
}

func TestRawSQLGuardPackageHasNoRawSQL(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		checked++
		if hits := rawSQLCalls(t, token.NewFileSet(), path, nil); len(hits) > 0 {
			t.Errorf("raw SQL in cmd/api/commands, route it through a repository: %v", hits)
		}
	}
	if checked == 0 {
		t.Fatal("no non-test files found; the guard checked nothing")
	}
	if _, err := os.Stat("common.go"); err != nil {
		t.Fatalf("guard is not running in the commands package: %v", err)
	}
}

func TestRawSQLGuardFixtures(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"exec on pool", "package p\nfunc f() { pool.Exec(ctx, \"UPDATE tracks SET x = 1\") }", 1},
		{"query row", "package p\nfunc f() { _ = db.QueryRow(ctx, \"SELECT 1\") }", 1},
		{"query", "package p\nfunc f() { _, _ = tx.Query(ctx, \"SELECT 1\") }", 1},
		{"begin", "package p\nfunc f() { _, _ = pool.Begin(ctx) }", 1},
		{"look-alike method", "package p\nfunc f() { store.Exists(ctx, ref) }", 0},
		{"string literal", "package p\nvar s = \"Query\"\nfunc f() { println(\"Exec Begin\") }", 0},
		{"empty file", "package p\n", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rawSQLCalls(t, token.NewFileSet(), "fixture.go", tc.src); len(got) != tc.want {
				t.Fatalf("got %d hits %v, want %d", len(got), got, tc.want)
			}
		})
	}
}
