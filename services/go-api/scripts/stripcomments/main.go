package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type deletion struct {
	start, end  int
	replacement string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

func run(args []string, stdout io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stdout, "usage: go run ./scripts/stripcomments <path>...")
		return 2
	}

	files, err := resolveFiles(args)
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "%v\n", err)
		return 2
	}

	strippedComments, strippedFiles, failed := stripAll(files, stdout)

	_, _ = fmt.Fprintf(stdout, "stripped %d comments in %d files\n", strippedComments, strippedFiles)
	if failed {
		return 1
	}
	return 0
}

func resolveFiles(args []string) ([]string, error) {
	var files []string
	for _, arg := range args {
		found, err := collectGoFiles(arg)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", arg, err)
		}
		files = append(files, found...)
	}
	return files, nil
}

func stripAll(files []string, stdout io.Writer) (comments, changedFiles int, failed bool) {
	for _, file := range files {
		n, ok := stripFile(file, stdout)
		if !ok {
			failed = true
			continue
		}
		if n > 0 {
			comments += n
			changedFiles++
		}
	}
	return comments, changedFiles, failed
}

func stripFile(file string, stdout io.Writer) (int, bool) {
	n, err := rewriteFile(file)
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "%s: %v\n", file, err)
		return 0, false
	}
	return n, true
}

func rewriteFile(file string) (int, error) {
	info, err := os.Stat(file)
	if err != nil {
		return 0, err
	}
	src, err := os.ReadFile(file)
	if err != nil {
		return 0, err
	}
	out, n, err := strip(src, file)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, nil
	}
	if err := os.WriteFile(file, out, info.Mode()); err != nil {
		return 0, err
	}
	return n, nil
}

func collectGoFiles(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		if !strings.HasSuffix(root, ".go") {
			return nil, fmt.Errorf("%s is not a .go file", root)
		}
		return []string{root}, nil
	}

	var files []string
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		return walkGoFile(path, d, err, &files)
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return files, nil
}

func walkGoFile(path string, d fs.DirEntry, err error, files *[]string) error {
	if err != nil {
		return err
	}
	if d.IsDir() {
		return skipUnwanted(d)
	}
	if strings.HasSuffix(path, ".go") {
		*files = append(*files, path)
	}
	return nil
}

func skipUnwanted(d fs.DirEntry) error {
	switch d.Name() {
	case "vendor", "testdata", "node_modules":
		return filepath.SkipDir
	}
	return nil
}

func strip(src []byte, filename string) ([]byte, int, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, 0, err
	}

	if err := refuseIfLossy(file); err != nil {
		return nil, 0, err
	}

	deletions := dropRanges(fset, src, file)

	formatted, err := format.Source(applyDeletions(src, deletions))
	if err != nil {
		return nil, 0, err
	}
	if isCRLF(src) {
		formatted = bytes.ReplaceAll(formatted, []byte("\n"), []byte("\r\n"))
	}
	return formatted, len(deletions), nil
}

func refuseIfLossy(file *ast.File) error {
	if hasLegacyBuildConstraint(file) && !hasGoBuildDirective(file) {
		return errStrippingChangesBuildSet
	}
	if hasExampleOutputComment(file) {
		return errStrippingDisablesExampleOutput
	}
	return nil
}

var (
	errStrippingChangesBuildSet       = errors.New("has a // +build constraint with no //go:build; stripping would change the build set")
	errStrippingDisablesExampleOutput = errors.New("has an Example Output comment; stripping would silently disable the test")
)

func hasGoBuildDirective(file *ast.File) bool {
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if strings.HasPrefix(comment.Text, "//go:build") {
				return true
			}
		}
	}
	return false
}

func hasLegacyBuildConstraint(file *ast.File) bool {
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if strings.HasPrefix(comment.Text, "// +build") {
				return true
			}
		}
	}
	return false
}

func hasExampleOutputComment(file *ast.File) bool {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "Example") {
			continue
		}
		for _, group := range file.Comments {
			for _, comment := range group.List {
				if comment.Pos() < fn.Body.Lbrace || comment.Pos() > fn.Body.Rbrace {
					continue
				}
				if isExampleOutputComment(comment.Text) {
					return true
				}
			}
		}
	}
	return false
}

func isExampleOutputComment(text string) bool {
	body := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(text, "//"), "/*"))
	return strings.HasPrefix(body, "Output:") || strings.HasPrefix(body, "Unordered output:")
}

func cgoPreambleGroup(file *ast.File) *ast.CommentGroup {
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT || gd.Doc == nil {
			continue
		}
		for _, spec := range gd.Specs {
			imp, ok := spec.(*ast.ImportSpec)
			if ok && imp.Path.Value == `"C"` {
				return gd.Doc
			}
		}
	}
	return nil
}

func isCRLF(src []byte) bool {
	return bytes.Contains(src, []byte("\r\n"))
}

func dropRanges(fset *token.FileSet, src []byte, file *ast.File) []deletion {
	preamble := cgoPreambleGroup(file)
	var deletions []deletion
	for _, group := range file.Comments {
		if group == preamble {
			continue
		}
		for _, comment := range group.List {
			if isKept(comment.Text) {
				continue
			}
			deletions = append(deletions, commentRange(fset, src, comment))
		}
	}
	sort.Slice(deletions, func(i, j int) bool { return deletions[i].start < deletions[j].start })
	return deletions
}

func applyDeletions(src []byte, deletions []deletion) []byte {
	var out bytes.Buffer
	cursor := 0
	for _, d := range deletions {
		if d.start < cursor {
			continue
		}
		out.Write(src[cursor:d.start])
		out.WriteString(d.replacement)
		cursor = d.end
	}
	out.Write(src[cursor:])
	return out.Bytes()
}

func isKept(text string) bool {
	if strings.HasPrefix(text, "//go:") {
		return true
	}
	if strings.HasPrefix(text, "//export ") {
		return true
	}
	if strings.HasPrefix(text, "//line ") {
		return true
	}
	return strings.Contains(text, "//nolint")
}

func commentRange(fset *token.FileSet, src []byte, comment *ast.Comment) deletion {
	start := fset.Position(comment.Pos()).Offset
	end := fset.Position(comment.End()).Offset
	lineStart := lineStartOffset(src, start)
	lineEnd := lineEndOffset(src, end)

	if isBlank(src[lineStart:start]) && isBlank(src[end:lineEnd]) {
		return wholeLineDeletion(src, lineStart, lineEnd)
	}
	return midLineDeletion(src, comment, lineStart, start, end)
}

func wholeLineDeletion(src []byte, lineStart, lineEnd int) deletion {
	if lineEnd < len(src) {
		return deletion{lineStart, lineEnd + 1, ""}
	}
	return deletion{lineStart, lineEnd, ""}
}

func midLineDeletion(src []byte, comment *ast.Comment, lineStart, start, end int) deletion {
	if strings.Contains(comment.Text, "\n") {
		return deletion{start, end, "\n"}
	}
	return deletion{trimTrailingSpace(src, lineStart, start), end, ""}
}

func lineStartOffset(src []byte, offset int) int {
	if idx := bytes.LastIndexByte(src[:offset], '\n'); idx >= 0 {
		return idx + 1
	}
	return 0
}

func lineEndOffset(src []byte, offset int) int {
	if idx := bytes.IndexByte(src[offset:], '\n'); idx >= 0 {
		return offset + idx
	}
	return len(src)
}

func isBlank(b []byte) bool {
	return len(bytes.TrimSpace(b)) == 0
}

func trimTrailingSpace(src []byte, floor, offset int) int {
	for offset > floor && (src[offset-1] == ' ' || src[offset-1] == '\t') {
		offset--
	}
	return offset
}
