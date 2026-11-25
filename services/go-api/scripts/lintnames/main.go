package main

import (
	"bytes"
	"context"
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
		fmt.Fprintln(os.Stderr, "usage: go run ./scripts/lintnames <base-ref>")
		os.Exit(2)
	}
	ctx := context.Background()
	base := os.Args[1]
	files := changedGoFiles(ctx, base)
	if len(files) == 0 {
		fmt.Println("No changed go files to check for vague identifier names.")
		return
	}
	fmt.Println("Checking added lines for vague identifier names in:")
	for _, file := range files {
		fmt.Printf("  %s\n", file.path)
	}
	hits := 0
	for _, file := range files {
		hits += reportFile(ctx, base, file)
	}
	fmt.Printf("new-code vague-name violations: %d\n", hits)
	if hits > 0 {
		os.Exit(1)
	}
}

type changedFile struct {
	path string
	from string
}

func changedGoFiles(ctx context.Context, base string) []changedFile {
	out := git(ctx, "diff", "-M", "--name-status", "--relative", "--diff-filter=ACMR", base, "--", ".")
	var files []changedFile
	for _, line := range strings.Split(out, "\n") {
		file := parseNameStatus(line)
		if strings.HasSuffix(file.path, ".go") && !strings.HasSuffix(file.path, "_test.go") {
			files = append(files, file)
		}
	}
	return files
}

func parseNameStatus(line string) changedFile {
	fields := strings.Split(line, "\t")
	file := changedFile{path: fields[len(fields)-1]}
	if len(fields) == 3 && strings.HasPrefix(fields[0], "R") {
		file.from = fields[1]
	}
	return file
}

func diffOf(ctx context.Context, base string, file changedFile) string {
	args := []string{"diff", "-M", "-U0", base, "--", file.path}
	if file.from != "" {
		args = append(args, file.from)
	}
	return git(ctx, args...)
}

func addedLines(ctx context.Context, base string, file changedFile) map[int]bool {
	added := map[int]bool{}
	for _, line := range strings.Split(diffOf(ctx, base, file), "\n") {
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

func reportFile(ctx context.Context, base string, file changedFile) int {
	added := addedLines(ctx, base, file)
	if len(added) == 0 {
		return 0
	}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file.path, nil, parser.SkipObjectResolution)
	if parsed == nil {
		fmt.Fprintf(os.Stderr, "parse %s: %v\n", file.path, err)
		os.Exit(2)
	}
	hits := 0
	for _, ident := range declaredNames(parsed) {
		hits += reportIdent(fset, file.path, added, ident)
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

func git(ctx context.Context, args ...string) string {
	cmd := exec.CommandContext(ctx, "git", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "git %s: %v\n", strings.Join(args, " "), err)
		os.Exit(2)
	}
	return out.String()
}
