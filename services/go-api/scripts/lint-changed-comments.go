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

var (
	hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)
	diffTarget = regexp.MustCompile(`^\+\+\+ b/(.+)$`)
)

func main() {
	if len(os.Args) < 2 || strings.HasPrefix(os.Args[1], "-") {
		fmt.Fprintln(os.Stderr, "usage: go run scripts/lint-changed-comments.go <base-ref> [module-dir]")
		os.Exit(2)
	}
	rawBase := os.Args[1]
	if len(os.Args) >= 3 {
		if err := os.Chdir(os.Args[2]); err != nil {
			fmt.Fprintf(os.Stderr, "chdir %s: %v\n", os.Args[2], err)
			os.Exit(2)
		}
	}
	base := resolveBase(rawBase)
	addedByFile := changedAddedLines(base)
	files := goFiles(addedByFile)
	if len(files) == 0 {
		fmt.Println("No changed go files to check for new comments.")
		return
	}
	fmt.Println("Checking added lines for new comments/suppressions in:")
	for _, file := range files {
		fmt.Printf("  %s\n", file)
	}
	hits := 0
	for _, file := range files {
		hits += reportFile(file, addedByFile[file])
	}
	fmt.Printf("new-code comment/suppression violations: %d\n", hits)
	if hits > 0 {
		os.Exit(1)
	}
}

func resolveBase(rawBase string) string {
	return strings.TrimSpace(git("rev-parse", "--verify", "--end-of-options", rawBase+"^{commit}"))
}

func changedAddedLines(base string) map[string]map[int]bool {
	byFile := map[string]map[int]bool{}
	var added map[int]bool
	for _, line := range strings.Split(git("diff", "-M", "-C", "--find-copies-harder", "-U0", "--relative", base), "\n") {
		if target := diffTarget.FindStringSubmatch(line); target != nil {
			path := strings.TrimSuffix(target[1], "\t")
			if strings.HasPrefix(path, "../") {
				added = nil
				continue
			}
			added = map[int]bool{}
			byFile[path] = added
			continue
		}
		match := hunkHeader.FindStringSubmatch(line)
		if match == nil || added == nil {
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
	return byFile
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

func reportFile(file string, added map[int]bool) int {
	if len(added) == 0 {
		return 0
	}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, nil, parser.ParseComments|parser.SkipObjectResolution)
	if parsed == nil {
		fmt.Fprintf(os.Stderr, "parse %s: %v\n", file, err)
		os.Exit(2)
	}
	hits := 0
	for _, group := range parsed.Comments {
		for _, comment := range group.List {
			hits += reportComment(fset, file, added, comment)
		}
	}
	return hits
}

func reportComment(fset *token.FileSet, file string, added map[int]bool, comment *ast.Comment) int {
	start := fset.Position(comment.Pos()).Line
	end := fset.Position(comment.End()).Line
	if !spansAddedLine(start, end, added) {
		return 0
	}
	kind := classify(comment.Text)
	if kind == "" {
		return 0
	}
	fmt.Printf("  %s:%d  new %s on a changed line — zero-comments rule\n", file, start, kind)
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
