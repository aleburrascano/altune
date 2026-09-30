package main

import (
	"bytes"
	"errors"
	"fmt"
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

type target struct {
	display string
	read    string
	kind    kind
	added   map[int]bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

func run(args []string, stdout io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stdout, "usage: go run ./scripts/nocomments <strip|check|diff> <path>...")
		return 2
	}
	switch args[0] {
	case "strip":
		return runStrip(args[1:], stdout)
	case "check":
		return runCheck(args[1:], stdout)
	case "diff":
		return runDiff(args[1:], stdout)
	default:
		_, _ = fmt.Fprintln(stdout, "usage: go run ./scripts/nocomments <strip|check|diff> <path>...")
		return 2
	}
}

func runStrip(args []string, stdout io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stdout, "usage: go run ./scripts/nocomments strip <path>...")
		return 2
	}
	targets, err := resolveTargets(args)
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "%v\n", err)
		return 2
	}
	comments, files, failed := stripAll(targets, stdout)
	_, _ = fmt.Fprintf(stdout, "stripped %d comments in %d files\n", comments, files)
	if failed {
		return 1
	}
	return 0
}

func stripAll(targets []target, stdout io.Writer) (comments, files int, failed bool) {
	for _, t := range targets {
		n, ok := stripTarget(t, stdout)
		if !ok {
			failed = true
			continue
		}
		if n > 0 {
			comments += n
			files++
		}
	}
	return comments, files, failed
}

func stripTarget(t target, stdout io.Writer) (int, bool) {
	info, err := os.Stat(t.read)
	if err != nil {
		return failTarget(t, stdout, err)
	}
	src, err := os.ReadFile(t.read)
	if err != nil {
		return failTarget(t, stdout, err)
	}
	out, n, err := strip(src, t.kind)
	if err != nil {
		return failTarget(t, stdout, err)
	}
	if n == 0 {
		return 0, true
	}
	if err := replaceFile(t.read, out, info.Mode()); err != nil {
		return failTarget(t, stdout, err)
	}
	return n, true
}

func replaceFile(path string, content []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".nocomments-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func failTarget(t target, stdout io.Writer, err error) (int, bool) {
	_, _ = fmt.Fprintf(stdout, "%s: %v\n", t.display, err)
	return 0, false
}

func strip(src []byte, k kind) ([]byte, int, error) {
	spans, err := k.comments(src)
	if err != nil {
		return nil, 0, err
	}
	if len(spans) == 0 {
		return src, 0, nil
	}
	out := deleteSpans(src, spans)
	if err := k.same(src, out); err != nil {
		return nil, 0, err
	}
	return out, len(spans), nil
}

func deleteSpans(src []byte, spans []span) []byte {
	sorted := append([]span(nil), spans...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].start < sorted[j].start })
	var out bytes.Buffer
	cursor := 0
	for _, s := range sorted {
		if s.start < cursor {
			continue
		}
		out.Write(src[cursor:s.start])
		cursor = s.end
	}
	out.Write(src[cursor:])
	return out.Bytes()
}

func runCheck(args []string, stdout io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stdout, "usage: go run ./scripts/nocomments check <path>...")
		return 2
	}
	targets, err := resolveTargets(args)
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "%v\n", err)
		return 2
	}
	violations, ok := reportTargets(targets, stdout)
	if !ok {
		return 2
	}
	return violationExit(stdout, violations)
}

func reportTargets(targets []target, stdout io.Writer) (int, bool) {
	violations := 0
	ok := true
	for _, t := range targets {
		n, err := reportTarget(t, stdout)
		if err != nil {
			_, _ = fmt.Fprintf(stdout, "%s: %v\n", t.display, err)
			ok = false
			continue
		}
		violations += n
	}
	return violations, ok
}

func violationExit(stdout io.Writer, violations int) int {
	_, _ = fmt.Fprintf(stdout, "comment violations: %d\n", violations)
	if violations > 0 {
		return 1
	}
	return 0
}

func reportTarget(t target, stdout io.Writer) (int, error) {
	src, err := os.ReadFile(t.read)
	if err != nil {
		return 0, err
	}
	lines, err := reportedLines(t.kind, src)
	if err != nil {
		return 0, err
	}
	hits := 0
	for _, line := range lines {
		if t.added != nil && !t.added[line] {
			continue
		}
		_, _ = fmt.Fprintf(stdout, "%s:%d\n", t.display, line)
		hits++
	}
	return hits, nil
}

func reportedLines(k kind, src []byte) ([]int, error) {
	spans, err := k.comments(src)
	if err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	for _, s := range spans {
		seen[s.line] = true
	}
	if k.leftovers != nil {
		extra, err := k.leftovers(src)
		if err != nil {
			return nil, err
		}
		for _, line := range extra {
			seen[line] = true
		}
	}
	lines := make([]int, 0, len(seen))
	for line := range seen {
		lines = append(lines, line)
	}
	sort.Ints(lines)
	return lines, nil
}

var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

func runDiff(args []string, stdout io.Writer) int {
	if len(args) != 1 {
		_, _ = fmt.Fprintln(stdout, "usage: go run ./scripts/nocomments diff <base-ref>")
		return 2
	}
	base := args[0]
	root, err := repoRoot()
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "%v\n", err)
		return 2
	}
	targets, err := diffTargets(root, base)
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "%v\n", err)
		return 2
	}
	violations, ok := reportTargets(targets, stdout)
	if !ok {
		return 2
	}
	return violationExit(stdout, violations)
}

func diffTargets(root, base string) ([]target, error) {
	changed, err := changedFiles(root, base)
	if err != nil {
		return nil, err
	}
	var targets []target
	for _, c := range changed {
		t, ok, err := diffTarget(root, base, c)
		if err != nil {
			return nil, err
		}
		if ok {
			targets = append(targets, t)
		}
	}
	return targets, nil
}

func diffTarget(root, base string, c change) (target, bool, error) {
	full := filepath.Join(root, c.path)
	k, ok, err := matchKind(full)
	if err != nil {
		return target{}, false, fmt.Errorf("%s: %w", c.path, err)
	}
	if !ok {
		return target{}, false, nil
	}
	added, err := addedLines(root, base, c)
	if err != nil {
		return target{}, false, err
	}
	return target{display: c.path, read: full, kind: k, added: added}, true, nil
}

func repoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --show-toplevel: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

type change struct {
	path string
	from string
}

func changedFiles(root, base string) ([]change, error) {
	out, err := gitIn(root, "diff", "-M", "-C", "--name-status", "--diff-filter=ACMRC", base)
	if err != nil {
		return nil, err
	}
	var changes []change
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		c, err := parseNameStatusLine(line)
		if err != nil {
			return nil, err
		}
		changes = append(changes, c)
	}
	return changes, nil
}

func parseNameStatusLine(line string) (change, error) {
	fields := strings.Split(line, "\t")
	status := fields[0]
	if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
		if len(fields) != 3 {
			return change{}, fmt.Errorf("unexpected name-status line: %q", line)
		}
		return change{path: fields[2], from: fields[1]}, nil
	}
	if len(fields) != 2 {
		return change{}, fmt.Errorf("unexpected name-status line: %q", line)
	}
	return change{path: fields[1]}, nil
}

func addedLines(root, base string, c change) (map[int]bool, error) {
	pathArgs := []string{c.path}
	if c.from != "" && c.from != c.path {
		pathArgs = []string{c.from, c.path}
	}
	args := append([]string{"diff", "-M", "-C", "-U0", base, "--"}, pathArgs...)
	out, err := gitIn(root, args...)
	if err != nil {
		return nil, err
	}
	added := map[int]bool{}
	for _, line := range strings.Split(out, "\n") {
		addHunkLines(added, line)
	}
	return added, nil
}

func addHunkLines(added map[int]bool, line string) {
	match := hunkHeader.FindStringSubmatch(line)
	if match == nil {
		return
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

func gitIn(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, errOut.String())
	}
	return out.String(), nil
}

func resolveTargets(args []string) ([]target, error) {
	var targets []target
	for _, arg := range args {
		found, err := collectTargets(arg)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", arg, err)
		}
		targets = append(targets, found...)
	}
	return targets, nil
}

func collectTargets(root string) ([]target, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return collectFileTarget(root)
	}
	var targets []target
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		return walkTarget(path, d, err, &targets)
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return targets, nil
}

func collectFileTarget(root string) ([]target, error) {
	k, ok, err := matchKind(root)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("no matching file kind")
	}
	return []target{{display: root, read: root, kind: k}}, nil
}

func walkTarget(path string, d fs.DirEntry, err error, targets *[]target) error {
	if err != nil {
		return err
	}
	if d.IsDir() {
		return skipUnwanted(d)
	}
	if !d.Type().IsRegular() {
		return nil
	}
	k, ok, err := matchKind(path)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if ok {
		*targets = append(*targets, target{display: path, read: path, kind: k})
	}
	return nil
}

func skipUnwanted(d fs.DirEntry) error {
	switch d.Name() {
	case "node_modules", "vendor", ".git":
		return filepath.SkipDir
	}
	return nil
}

func matchKind(path string) (kind, bool, error) {
	head, err := readHead(path)
	if err != nil {
		return kind{}, false, fmt.Errorf("read: %w", err)
	}
	for _, k := range kinds {
		if k.match(path, head) {
			return k, true, nil
		}
	}
	return kind{}, false, nil
}

func readHead(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:n], nil
}
