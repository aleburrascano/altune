// Package guard holds architectural invariant tests that assert Overseer's build
// graph, not its runtime behaviour.
package guard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// needle is built by concatenation, not as a single literal, so this file's own
// non-test source never trips the scan it defines — the needle's text never
// appears contiguously as a quoted Go string literal.
var needle = "/" + "admin"

// AdminPathLiterals walks every non-test .go file under root and returns, for
// each file containing a string literal naming an "/admin" path, the file path
// and the offending literal. Overseer's go-api surface moved from "/admin/*" to
// "/observe/*"; this is the source-scanning check that keeps it moved, per
// TestNoAdminPaths in no_admin_paths_test.go.
func AdminPathLiterals(root string) (map[string][]string, error) {
	hits := map[string][]string{}
	fset := token.NewFileSet()
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if strings.Contains(lit.Value, needle) {
				hits[path] = append(hits[path], lit.Value)
			}
			return true
		})
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return hits, nil
}
