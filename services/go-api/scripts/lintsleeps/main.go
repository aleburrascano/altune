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

const usage = "usage: go run ./scripts/lintsleeps [--summary] <dir>...\n  lists every time.Sleep call in the go files under each dir that is not inside a synctest.Test or synctest.Run callback, and exits 1 when there are any; internal/shared/clock, which wraps the real sleep, is exempt; --summary prints only the count per directory"

var skippedDirs = map[string]bool{"vendor": true, "testdata": true, "node_modules": true}

const exemptDir = "internal/shared/clock"

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
			say(stderr, "lintsleeps: %v\n", err)
			return 2
		}
		found = append(found, hits...)
	}
	if summary {
		printSummary(stdout, found)
	} else {
		for _, v := range found {
			say(stdout, "%s:%d: time.Sleep outside a synctest bubble; wait through the Sleeper in internal/shared/clock, or run the test inside synctest.Test\n", v.path, v.line)
		}
	}
	say(stdout, "time.Sleep calls outside a synctest bubble: %d\n", len(found))
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

func isExempt(path string) bool {
	return strings.Contains("/"+filepath.ToSlash(filepath.Dir(path))+"/", "/"+exemptDir+"/")
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
		if !strings.HasSuffix(path, ".go") || isExempt(path) {
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

func importName(file *ast.File, importPath, fallback string) string {
	for _, imp := range file.Imports {
		if strings.Trim(imp.Path.Value, `"`) != importPath {
			continue
		}
		switch {
		case imp.Name == nil:
			return fallback
		case imp.Name.Name == "_" || imp.Name.Name == ".":
			return ""
		default:
			return imp.Name.Name
		}
	}
	return ""
}

func scanFile(path string) ([]violation, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	timePkg := importName(file, "time", "time")
	if timePkg == "" {
		return nil, nil
	}
	synctestPkg := importName(file, "testing/synctest", "synctest")
	var found []violation
	var walk func(n ast.Node)
	walk = func(n ast.Node) {
		ast.Inspect(n, func(m ast.Node) bool {
			call, ok := m.(*ast.CallExpr)
			if !ok {
				return true
			}
			if isSelector(call.Fun, timePkg, "Sleep") {
				found = append(found, violation{path, fset.Position(call.Pos()).Line})
				return true
			}
			if synctestPkg != "" && (isSelector(call.Fun, synctestPkg, "Test") || isSelector(call.Fun, synctestPkg, "Run")) {
				for _, arg := range call.Args {
					if _, isLit := arg.(*ast.FuncLit); !isLit {
						walk(arg)
					}
				}
				return false
			}
			return true
		})
	}
	walk(file)
	return found, nil
}

func isSelector(e ast.Expr, pkg, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg && sel.Sel.Name == name
}
