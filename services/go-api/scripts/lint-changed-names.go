//go:build ignore

package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

var banned = map[string]bool{
	"data":    true,
	"util":    true,
	"helper":  true,
	"manager": true,
	"temp":    true,
	"arr":     true,
	"val":     true,
	"foo":     true,
	"doWork":  true,
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run scripts/lint-changed-names.go <base-ref>")
		os.Exit(2)
	}
	base := os.Args[1]
	files := changedGoFiles(base)
	if len(files) == 0 {
		fmt.Println("No changed go files to check for vague identifier names.")
		return
	}
	fmt.Println("Checking added lines for vague identifier names in:")
	for _, file := range files {
		fmt.Printf("  %s\n", file)
	}
	hits := 0
	for _, file := range files {
		hits += reportFile(base, file)
	}
	fmt.Printf("new-code vague-name violations: %d\n", hits)
	if hits > 0 {
		os.Exit(1)
	}
}

func changedGoFiles(base string) []string {
	out := git("diff", "--name-only", "--relative", "--diff-filter=ACMR", base, "--", ".")
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasSuffix(line, ".go") && !strings.HasSuffix(line, "_test.go") {
			files = append(files, line)
		}
	}
	return files
}

func addedLines(base, file string) map[int]bool {
	added := map[int]bool{}
	for _, line := range strings.Split(git("diff", "-U0", base, "--", file), "\n") {
		match := hunkHeader.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		start, _ := strconv.Atoi(match[1])
		count := 1
		if match[2] != "" {
			count, _ = strconv.Atoi(match[2])
		}
		for n := start; n < start+count; n++ {
			added[n] = true
		}
	}
	return added
}

func reportFile(base, file string) int {
	added := addedLines(base, file)
	if len(added) == 0 {
		return 0
	}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
	if parsed == nil {
		fmt.Fprintf(os.Stderr, "parse %s: %v\n", file, err)
		os.Exit(2)
	}
	hits := 0
	for _, ident := range declaredNames(parsed) {
		hits += reportIdent(fset, file, added, ident)
	}
	return hits
}

func reportIdent(fset *token.FileSet, file string, added map[int]bool, ident *ast.Ident) int {
	if !banned[ident.Name] {
		return 0
	}
	line := fset.Position(ident.Pos()).Line
	if !added[line] {
		return 0
	}
	fmt.Printf("  %s:%d  vague identifier %q — ban vague names (match id-denylist)\n", file, line, ident.Name)
	return 1
}

func declaredNames(file *ast.File) []*ast.Ident {
	var names []*ast.Ident
	collect := func(idents []*ast.Ident) {
		for _, ident := range idents {
			if ident != nil && ident.Name != "_" {
				names = append(names, ident)
			}
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.ValueSpec:
			collect(n.Names)
		case *ast.AssignStmt:
			if n.Tok == token.DEFINE {
				collect(lhsIdents(n.Lhs))
			}
		case *ast.RangeStmt:
			if n.Tok == token.DEFINE {
				collect(lhsIdents([]ast.Expr{n.Key, n.Value}))
			}
		case *ast.FuncDecl:
			collect([]*ast.Ident{n.Name})
		case *ast.FuncType:
			collect(fieldNames(n.Params))
			collect(fieldNames(n.Results))
		}
		return true
	})
	sort.Slice(names, func(i, j int) bool { return names[i].Pos() < names[j].Pos() })
	return names
}

func lhsIdents(exprs []ast.Expr) []*ast.Ident {
	var idents []*ast.Ident
	for _, expr := range exprs {
		if ident, ok := expr.(*ast.Ident); ok {
			idents = append(idents, ident)
		}
	}
	return idents
}

func fieldNames(fields *ast.FieldList) []*ast.Ident {
	if fields == nil {
		return nil
	}
	var idents []*ast.Ident
	for _, field := range fields.List {
		idents = append(idents, field.Names...)
	}
	return idents
}

func git(args ...string) string {
	cmd := exec.Command("git", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "git %s: %v\n", strings.Join(args, " "), err)
		os.Exit(2)
	}
	return out.String()
}
