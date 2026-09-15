package guard_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// bucketDirs returns the on-disk directory of every concrete bucket package.
func bucketDirs(t *testing.T) []string {
	t.Helper()
	out, err := exec.Command("go", "list", "-f", "{{.Dir}}", "altune/overseer/internal/buckets/...").Output()
	if err != nil {
		t.Fatalf("go list buckets: %v", err)
	}
	dirs := strings.Fields(string(out))
	if len(dirs) == 0 {
		t.Fatal("go list returned no bucket packages")
	}
	return dirs
}

// Auth invariant: no bucket constructs goapi.StaticTokenSource directly. Every
// bucket that builds a go-api client or SSE consumer must take its operator
// credential from goapi.SharedTokenSource, so the whole fleet shares ONE token
// source — that is what makes the refreshing source's single-flight span buckets
// and what activates refresh when the OVERSEER_SUPABASE_* / OVERSEER_GOAPI_REFRESH_TOKEN
// vars are set. A bucket that reaches for StaticTokenSource again would silently
// re-fragment the fleet's auth and pin it to a token that never refreshes; this
// guard fails the build before that ships.
func TestBucketsDoNotConstructStaticTokenSource(t *testing.T) {
	var (
		scanned    int
		usesShared bool
		usesStatic []string // "file:line" of any StaticTokenSource construction
		fset       = token.NewFileSet()
	)

	for _, dir := range bucketDirs(t) {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatalf("glob %s: %v", dir, err)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, file, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", file, err)
			}
			scanned++
			ast.Inspect(f, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok || pkg.Name != "goapi" {
					return true
				}
				switch sel.Sel.Name {
				case "StaticTokenSource":
					usesStatic = append(usesStatic, fset.Position(sel.Pos()).String())
				case "SharedTokenSource":
					usesShared = true
				}
				return true
			})
		}
	}

	if scanned == 0 {
		t.Fatal("scanned no bucket source files; guard would pass vacuously")
	}
	for _, at := range usesStatic {
		t.Errorf("bucket constructs goapi.StaticTokenSource directly at %s; use goapi.SharedTokenSource so the whole fleet shares one refreshing token source", at)
	}
	if !usesShared {
		t.Error("no bucket references goapi.SharedTokenSource; the shared-token-source adoption is not wired, so the guard cannot be trusted")
	}
}
