package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const usage = "usage: go run ./scripts/lintdoors [--summary] [<go-api dir>]\n  every internal/<module> has one door, its root package; lists each import from outside a module of one of its non-root packages and exits 1 when there are any; --summary prints, per module, its open packages with their importers and the exported identifiers of its root"

type violation struct {
	file     string
	line     int
	pkg      string
	module   string
	importer string
	test     bool
}

type scan struct {
	modulePath string
	violations []violation
	modules    map[string]bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func say(w io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(w, format, a...)
}

func run(args []string, stdout, stderr io.Writer) int {
	summary := false
	root := "."
	for _, arg := range args {
		switch {
		case arg == "--summary":
			summary = true
		case strings.HasPrefix(arg, "-"):
			say(stderr, "%s\n", usage)
			return 2
		default:
			root = arg
		}
	}
	s, err := scanTree(root)
	if err != nil {
		say(stderr, "lintdoors: %v\n", err)
		return 2
	}
	if summary {
		if err := printSummary(stdout, root, s); err != nil {
			say(stderr, "lintdoors: %v\n", err)
			return 2
		}
	} else {
		for _, v := range s.violations {
			say(stdout, "%s:%d: imports %s, a non-root package of module %s; import %s/internal/%s and move what is needed behind its root\n", v.file, v.line, v.pkg, v.module, s.modulePath, v.module)
		}
	}
	say(stdout, "imports through a module's side door: %d\n", len(s.violations))
	if len(s.violations) > 0 {
		return 1
	}
	return 0
}

func modulePathOf(root string) (string, error) {
	goMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(goMod), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(rest), nil
		}
	}
	return "", fmt.Errorf("no module line in %s", filepath.Join(root, "go.mod"))
}

func scanTree(root string) (*scan, error) {
	modulePath, err := modulePathOf(root)
	if err != nil {
		return nil, err
	}
	s := &scan{modulePath: modulePath, modules: map[string]bool{}}
	entries, err := os.ReadDir(filepath.Join(root, "internal"))
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			s.modules[e.Name()] = true
		}
	}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "vendor", "testdata", "node_modules", ".git":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		return s.scanFile(root, p)
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(s.violations, func(i, j int) bool {
		a, b := s.violations[i], s.violations[j]
		if a.file != b.file {
			return a.file < b.file
		}
		return a.line < b.line
	})
	return s, nil
}

func (s *scan) scanFile(root, file string) error {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
	if err != nil {
		return fmt.Errorf("parse %s: %w", file, err)
	}
	rel, err := filepath.Rel(root, filepath.Dir(file))
	if err != nil {
		return err
	}
	importer := filepath.ToSlash(rel)
	ownModule := ""
	if rest, ok := strings.CutPrefix(importer, "internal/"); ok {
		ownModule, _, _ = strings.Cut(rest, "/")
	}
	prefix := s.modulePath + "/internal/"
	for _, imp := range f.Imports {
		target, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		rest, ok := strings.CutPrefix(target, prefix)
		if !ok {
			continue
		}
		module, sub, _ := strings.Cut(rest, "/")
		if sub == "" || module == ownModule || !s.modules[module] {
			continue
		}
		s.violations = append(s.violations, violation{
			file:     file,
			line:     fset.Position(imp.Pos()).Line,
			pkg:      target,
			module:   module,
			importer: importer,
			test:     strings.HasSuffix(file, "_test.go"),
		})
	}
	return nil
}

func printSummary(w io.Writer, root string, s *scan) error {
	modules := make([]string, 0, len(s.modules))
	for m := range s.modules {
		modules = append(modules, m)
	}
	sort.Strings(modules)
	for _, m := range modules {
		exported, err := rootExports(filepath.Join(root, "internal", m))
		if err != nil {
			return err
		}
		open := map[string]map[string]int{}
		for _, v := range s.violations {
			if v.module != m {
				continue
			}
			sub := strings.TrimPrefix(v.pkg, s.modulePath+"/internal/")
			if open[sub] == nil {
				open[sub] = map[string]int{}
			}
			open[sub][v.importer]++
		}
		say(w, "%s: %d open non-root packages, %d exported identifiers at the root\n", m, len(open), exported)
		pkgs := make([]string, 0, len(open))
		for p := range open {
			pkgs = append(pkgs, p)
		}
		sort.Strings(pkgs)
		for _, p := range pkgs {
			importers := make([]string, 0, len(open[p]))
			for i := range open[p] {
				importers = append(importers, i)
			}
			sort.Strings(importers)
			exports, err := rootExports(filepath.Join(root, "internal", filepath.FromSlash(p)))
			if err != nil {
				return err
			}
			say(w, "  %s (%d exported identifiers)\n", p, exports)
			for _, i := range importers {
				say(w, "    %3d  %s\n", open[p][i], i)
			}
		}
	}
	return nil
}

func rootExports(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, 0)
		if err != nil {
			return 0, fmt.Errorf("parse %s: %w", path.Join(dir, name), err)
		}
		count += exportedIn(f)
	}
	return count, nil
}

func exportedIn(f *ast.File) int {
	count := 0
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil && d.Name.IsExported() {
				count++
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch sp := spec.(type) {
				case *ast.TypeSpec:
					if sp.Name.IsExported() {
						count++
					}
				case *ast.ValueSpec:
					for _, n := range sp.Names {
						if n.IsExported() {
							count++
						}
					}
				}
			}
		}
	}
	return count
}
