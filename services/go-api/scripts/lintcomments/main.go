package main

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	hunkHeader  = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)
	diffGitLine = regexp.MustCompile(`(?m)^diff --git .*$`)
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout))
}

func say(w io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(w, format, a...)
}

func run(ctx context.Context, args []string, stdout io.Writer) int {
	if len(args) >= 1 && args[0] == "--all" {
		return runAll(args[1:], stdout)
	}
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(os.Stderr, "usage: go run ./scripts/lintcomments <base-ref> [module-dir]\n       go run ./scripts/lintcomments --all <dir>...")
		return 2
	}
	return runDiff(ctx, args, stdout)
}

func runDiff(ctx context.Context, args []string, stdout io.Writer) int {
	rawBase := args[0]
	if len(args) >= 2 {
		if err := os.Chdir(args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "chdir %s: %v\n", args[1], err)
			return 2
		}
	}
	base, err := resolveBase(ctx, rawBase)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	addedByFile, err := changedAddedLines(ctx, base)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	files := goFiles(addedByFile)
	if len(files) == 0 {
		say(stdout, "No changed go files to check for new comments.\n")
		return 0
	}
	say(stdout, "Checking added lines for new comments/suppressions in:\n")
	for _, file := range files {
		say(stdout, "  %s\n", file)
	}
	hits := 0
	for _, file := range files {
		fileHits, err := reportFile(stdout, file, addedByFile[file])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		hits += fileHits
	}
	say(stdout, "new-code comment/suppression violations: %d\n", hits)
	if hits > 0 {
		return 1
	}
	return 0
}

var skippedDirs = map[string]bool{"vendor": true, "testdata": true, "node_modules": true}

type violation struct {
	path string
	line int
}

func runAll(dirs []string, stdout io.Writer) int {
	if len(dirs) == 0 {
		fmt.Fprintln(os.Stderr, "usage: go run ./scripts/lintcomments --all <dir>...")
		return 2
	}
	var found []violation
	for _, dir := range dirs {
		hits, err := commentsUnder(dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		found = append(found, hits...)
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].path != found[j].path {
			return found[i].path < found[j].path
		}
		return found[i].line < found[j].line
	})
	for _, hit := range found {
		say(stdout, "%s:%d\n", hit.path, hit.line)
	}
	say(stdout, "comment violations: %d\n", len(found))
	if len(found) > 0 {
		return 1
	}
	return 0
}

func commentsUnder(dir string) ([]violation, error) {
	var found []violation
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skippedDirs[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		hits, err := commentsIn(path)
		found = append(found, hits...)
		return err
	})
	return found, err
}

func commentsIn(path string) ([]violation, error) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var found []violation
	for _, group := range parsed.Comments {
		for _, comment := range group.List {
			if !isEmbed(comment.Text) {
				found = append(found, violation{path, fset.Position(comment.Pos()).Line})
			}
		}
	}
	return found, nil
}

func isEmbed(text string) bool {
	fields := strings.Fields(text)
	return len(fields) > 0 && fields[0] == "//go:embed"
}

func resolveBase(ctx context.Context, rawBase string) (string, error) {
	out, err := git(ctx, "rev-parse", "--verify", "--end-of-options", rawBase+"^{commit}")
	return strings.TrimSpace(out), err
}

func changedAddedLines(ctx context.Context, base string) (map[string]map[int]bool, error) {
	byFile := map[string]map[int]bool{}
	nameList, err := git(ctx, "diff", "--text", "--name-only", "-z", "--diff-filter=ACMR", "-M", "-C", "--find-copies-harder", "--relative", base)
	if err != nil {
		return nil, err
	}
	names := splitNUL(nameList)
	if len(names) == 0 {
		return byFile, nil
	}
	patch, err := git(ctx, "diff", "--text", "--diff-filter=ACMR", "-M", "-C", "--find-copies-harder", "-U0", "--relative", base)
	if err != nil {
		return nil, err
	}
	blocks := splitDiffBlocks(patch)
	for i, path := range names {
		if strings.HasPrefix(path, "../") {
			continue
		}
		block := ""
		if i < len(blocks) {
			block = blocks[i]
		}
		byFile[path] = hunksOf(block)
	}
	return byFile, nil
}

func splitNUL(s string) []string {
	parts := strings.Split(strings.TrimSuffix(s, "\x00"), "\x00")
	if len(parts) == 1 && parts[0] == "" {
		return nil
	}
	return parts
}

func splitDiffBlocks(patch string) []string {
	idx := diffGitLine.FindAllStringIndex(patch, -1)
	blocks := make([]string, 0, len(idx))
	for i, loc := range idx {
		end := len(patch)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		blocks = append(blocks, patch[loc[1]:end])
	}
	return blocks
}

func hunksOf(block string) map[int]bool {
	added := map[int]bool{}
	for _, line := range strings.Split(block, "\n") {
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

func goFiles(addedByFile map[string]map[int]bool) []string {
	var files []string
	for file := range addedByFile {
		if strings.HasSuffix(file, ".go") {
			files = append(files, file)
		}
	}
	sort.Strings(files)
	return files
}

func reportFile(stdout io.Writer, file string, added map[int]bool) (int, error) {
	if len(added) == 0 {
		return 0, nil
	}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, nil, parser.ParseComments|parser.SkipObjectResolution)
	if parsed == nil {
		return 0, fmt.Errorf("parse %s: %w", file, err)
	}
	hits := 0
	for _, group := range parsed.Comments {
		for _, comment := range group.List {
			hits += reportComment(stdout, fset, file, added, comment)
		}
	}
	return hits, nil
}

func reportComment(stdout io.Writer, fset *token.FileSet, file string, added map[int]bool, comment *ast.Comment) int {
	start := fset.Position(comment.Pos()).Line
	end := fset.Position(comment.End()).Line
	if !spansAddedLine(start, end, added) {
		return 0
	}
	kind := classify(comment.Text)
	if kind == "" {
		return 0
	}
	say(stdout, "  %s:%d  new %s on a changed line — zero-comments rule\n", file, start, kind)
	return 1
}

func classify(text string) string {
	body, isLine := strings.CutPrefix(text, "//")
	if !isLine {
		return "comment"
	}
	if isSuppression(body) {
		return "suppression"
	}
	if isDirective(body) {
		return ""
	}
	return "comment"
}

func isSuppression(body string) bool {
	return strings.HasPrefix(body, "nolint") || strings.HasPrefix(body, "lint:")
}

func isDirective(body string) bool {
	return strings.HasPrefix(body, "go:") || strings.HasPrefix(body, "line ")
}

func spansAddedLine(start, end int, added map[int]bool) bool {
	for n := start; n <= end; n++ {
		if added[n] {
			return true
		}
	}
	return false
}

func git(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.quotePath=false"}, args...)...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return out.String(), nil
}
