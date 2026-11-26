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
	var dirs []string
	for _, arg := range args {
		switch {
		case arg == "--summary":
			summary = true
		case strings.HasPrefix(arg, "-"):
			say(stderr, "%s\n", usage)
			return 2
		default:
			dirs = append(dirs, arg)
		}
	}
	if len(dirs) == 0 {
		say(stderr, "%s\n", usage)
		return 2
	}
	var found []violation
	for _, dir := range dirs {
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
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	var found []violation
	err = filepath.WalkDir(resolved, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != resolved && skippedDirs[d.Name()] {
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
	pkg, ok := timeImportName(file)
	if !ok {
		return nil, nil
	}
	c := checker{pkg: pkg}
	var found []violation
	for _, decl := range file.Decls {
		ast.Inspect(decl, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.GenDecl:
				return v.Tok != token.CONST
			case *ast.BinaryExpr:
				if v.Op == token.MUL && c.isNumberTimesUnit(v) {
					found = append(found, violation{path, fset.Position(v.Pos()).Line})
				}
			}
			return true
		})
	}
	return found, nil
}

func timeImportName(file *ast.File) (string, bool) {
	for _, imp := range file.Imports {
		if imp.Path.Value != `"time"` {
			continue
		}
		switch {
		case imp.Name == nil:
			return "time", true
		case imp.Name.Name == "_":
			return "", false
		default:
			return imp.Name.Name, true
		}
	}
	return "", false
}

var numericTypes = map[string]bool{
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"float32": true, "float64": true,
}

type checker struct {
	pkg string
}

func (c checker) isNumberTimesUnit(bin *ast.BinaryExpr) bool {
	return (c.isNumber(bin.X) && c.isUnit(bin.Y)) || (c.isNumber(bin.Y) && c.isUnit(bin.X))
}

func (c checker) isNumber(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.BasicLit:
		return v.Kind == token.INT || v.Kind == token.FLOAT
	case *ast.ParenExpr:
		return c.isNumber(v.X)
	case *ast.UnaryExpr:
		return (v.Op == token.SUB || v.Op == token.ADD) && c.isNumber(v.X)
	case *ast.BinaryExpr:
		return c.isNumber(v.X) && c.isNumber(v.Y)
	case *ast.CallExpr:
		return len(v.Args) == 1 && c.isNumericConversion(v.Fun) && c.isNumber(v.Args[0])
	}
	return false
}

func (c checker) isNumericConversion(fun ast.Expr) bool {
	if id, ok := fun.(*ast.Ident); ok {
		return numericTypes[id.Name]
	}
	return c.isTimeSelector(fun, "Duration")
}

func (c checker) isUnit(e ast.Expr) bool {
	if p, ok := e.(*ast.ParenExpr); ok {
		return c.isUnit(p.X)
	}
	return c.isTimeSelector(e, "")
}

func (c checker) isTimeSelector(e ast.Expr, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != c.pkg {
		return false
	}
	if name != "" {
		return sel.Sel.Name == name
	}
	return timeUnits[sel.Sel.Name]
}
