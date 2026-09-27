package main

import (
	"bytes"
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
	start, end int
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

	deletions := dropRanges(fset, src, file)

	formatted, err := format.Source(applyDeletions(src, deletions))
	if err != nil {
		return nil, 0, err
	}
	return formatted, len(deletions), nil
}

func dropRanges(fset *token.FileSet, src []byte, file *ast.File) []deletion {
	var deletions []deletion
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if isKept(comment.Text) {
				continue
			}
			start, end := commentRange(fset, src, comment)
			deletions = append(deletions, deletion{start, end})
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
		cursor = d.end
	}
	out.Write(src[cursor:])
	return out.Bytes()
}

func isKept(text string) bool {
	if strings.HasPrefix(text, "//go:") {
		return true
	}
	return strings.Contains(text, "//nolint")
}

func commentRange(fset *token.FileSet, src []byte, comment *ast.Comment) (int, int) {
	start := fset.Position(comment.Pos()).Offset
	end := fset.Position(comment.End()).Offset

	lineStart := lineStartOffset(src, start)
	lineEnd := lineEndOffset(src, end)

	alone := isBlank(src[lineStart:start])
	aloneEnd := isBlank(src[end:lineEnd])

	if alone && aloneEnd {
		if lineEnd < len(src) {
			return lineStart, lineEnd + 1
		}
		return lineStart, lineEnd
	}

	return trimTrailingSpace(src, lineStart, start), end
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
