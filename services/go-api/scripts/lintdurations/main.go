package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const usage = "usage: go run ./scripts/lintdurations [--summary] <dir>...\n  lists every inline duration literal (a number times a time unit, outside a const declaration) in the non-test go files under each dir, and exits 1 when there are any; --summary prints only the count per directory"

var skippedDirs = map[string]bool{"vendor": true, "testdata": true, "node_modules": true}

var timeUnits = map[string]bool{
	"Nanosecond":  true,
	"Microsecond": true,
	"Millisecond": true,
	"Second":      true,
	"Minute":      true,
	"Hour":        true,
}

type violation struct {
	path string
	line int
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func say(w io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(w, format, a...)
}

func run(args []string, stdout, stderr io.Writer) int {
	summary := false
	if len(args) > 0 && args[0] == "--summary" {
		summary = true
		args = args[1:]
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		say(stderr, "%s\n", usage)
		return 2
	}
	var found []violation
	for _, dir := range args {
		hits, err := scanDir(dir)
		if err != nil {
			say(stderr, "lintdurations: %v\n", err)
			return 2
		}
		found = append(found, hits...)
	}
	if summary {
		printSummary(stdout, found)
	} else {
		for _, v := range found {
			say(stdout, "%s:%d: inline duration literal; name it as a const or take it from config\n", v.path, v.line)
		}
	}
	say(stdout, "inline duration literals: %d\n", len(found))
	if len(found) > 0 {
		return 1
	}
	return 0
}

func printSummary(w io.Writer, found []violation) {
	perDir := map[string]int{}
	for _, v := range found {
		perDir[filepath.Dir(v.path)]++
	}
	dirs := make([]string, 0, len(perDir))
	for d := range perDir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		say(w, "%6d  %s\n", perDir[d], d)
	}
}

func scanDir(root string) ([]violation, error) {
	var found []violation
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skippedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		hits, err := scanFile(path)
		if err != nil {
			return err
		}
		found = append(found, hits...)
		return nil
	})
	return found, err
}

func scanFile(path string) ([]violation, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var found []violation
	for _, decl := range file.Decls {
		ast.Inspect(decl, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.GenDecl:
				return v.Tok != token.CONST
			case *ast.BinaryExpr:
				if v.Op == token.MUL && isLiteralTimesUnit(v) {
					found = append(found, violation{path, fset.Position(v.Pos()).Line})
				}
			}
			return true
		})
	}
	return found, nil
}

func isLiteralTimesUnit(bin *ast.BinaryExpr) bool {
	return (isNumber(bin.X) && isUnit(bin.Y)) || (isNumber(bin.Y) && isUnit(bin.X))
}

func isNumber(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.BasicLit:
		return v.Kind == token.INT || v.Kind == token.FLOAT
	case *ast.ParenExpr:
		return isNumber(v.X)
	case *ast.CallExpr:
		return isTimeSelector(v.Fun, "Duration") && len(v.Args) == 1 && isNumber(v.Args[0])
	}
	return false
}

func isUnit(e ast.Expr) bool {
	if p, ok := e.(*ast.ParenExpr); ok {
		return isUnit(p.X)
	}
	return isTimeSelector(e, "")
}

func isTimeSelector(e ast.Expr, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "time" {
		return false
	}
	if name != "" {
		return sel.Sel.Name == name
	}
	return timeUnits[sel.Sel.Name]
}
