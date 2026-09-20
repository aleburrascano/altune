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
	"strconv"
	"strings"
)

var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run scripts/lint-changed-comments.go <base-ref>")
		os.Exit(2)
	}
	base := os.Args[1]
	files := changedGoFiles(base)
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
		hits += reportFile(base, file)
	}
	fmt.Printf("new-code comment/suppression violations: %d\n", hits)
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
	if isLine && strings.HasPrefix(body, "nolint") {
		return "suppression"
	}
	if isLine && isDirective(body) {
		return ""
	}
	return "comment"
}

func isDirective(body string) bool {
	if strings.HasPrefix(body, "line ") {
		return true
	}
	colon := strings.Index(body, ":")
	if colon <= 0 || colon+1 >= len(body) {
		return false
	}
	for i := 0; i <= colon+1; i++ {
		if i == colon {
			continue
		}
		if b := body[i]; !('a' <= b && b <= 'z' || '0' <= b && b <= '9') {
			return false
		}
	}
	return true
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
