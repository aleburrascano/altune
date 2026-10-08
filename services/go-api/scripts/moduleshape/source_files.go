package main

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
)

type sourceFile struct {
	rel  string
	top  string
	test bool
	file *ast.File
}

type callSite struct {
	fn   string
	name string
}

func checkSources(root string) ([]string, error) {
	files, err := parseInternal(root)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, rule := range []func([]sourceFile) []string{checkHTTPError, checkCodedErrors, checkHandlerNames, checkPlainLogs, checkFakes} {
		lines = append(lines, rule(files)...)
	}
	return lines, nil
}

func parseInternal(root string) ([]sourceFile, error) {
	var files []sourceFile
	fset := token.NewFileSet()
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() && skipEntry(entry.Name()) {
			return skipSource(entry, err)
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}
		parsed, err := parseSource(fset, root, path)
		files = append(files, parsed)
		return err
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return files, err
}

func skipSource(entry fs.DirEntry, err error) error {
	if err != nil {
		return err
	}
	return fs.SkipDir
}

func parseSource(fset *token.FileSet, root, path string) (sourceFile, error) {
	parsed, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return sourceFile{}, err
	}
	rel, err := filepath.Rel(root, path)
	rel = filepath.ToSlash(rel)
	top, _, _ := strings.Cut(strings.TrimPrefix(rel, "internal/"), "/")
	return sourceFile{rel, top, strings.HasSuffix(rel, "_test.go"), parsed}, err
}

func selectorCalls(file sourceFile, pkg string, names ...string) []callSite {
	var sites []callSite
	for _, decl := range file.file.Decls {
		ast.Inspect(decl, func(node ast.Node) bool {
			if name, ok := calledSelector(node, pkg); ok && contains(names, name) {
				sites = append(sites, callSite{declName(decl), pkg + "." + name})
			}
			return true
		})
	}
	return sites
}

func calledSelector(node ast.Node, pkg string) (string, bool) {
	call, ok := node.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	owner, ok := sel.X.(*ast.Ident)
	return sel.Sel.Name, ok && owner.Name == pkg
}

func contains(names []string, name string) bool {
	for _, candidate := range names {
		if candidate == name {
			return true
		}
	}
	return false
}

func declName(decl ast.Decl) string {
	if fn, ok := decl.(*ast.FuncDecl); ok {
		return fn.Name.Name
	}
	return "package"
}

func receiverName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	kind := fn.Recv.List[0].Type
	if star, ok := kind.(*ast.StarExpr); ok {
		kind = star.X
	}
	if id, ok := kind.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}
