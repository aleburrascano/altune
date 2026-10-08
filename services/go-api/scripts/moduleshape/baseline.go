package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

func normalize(lines []string) []string {
	seen := map[string]bool{}
	var kept []string
	for _, line := range lines {
		if line != "" && !seen[line] {
			seen[line] = true
			kept = append(kept, line)
		}
	}
	sort.Strings(kept)
	return kept
}

func readBaseline(path string) ([]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return normalize(strings.Split(string(content), "\n")), nil
}

func writeBaseline(path string, lines []string) error {
	kept := normalize(lines)
	body := ""
	if len(kept) > 0 {
		body = strings.Join(kept, "\n") + "\n"
	}
	return os.WriteFile(path, []byte(body), 0o644)
}

func missingFrom(lines, other []string) []string {
	known := map[string]bool{}
	for _, line := range other {
		known[line] = true
	}
	var missing []string
	for _, line := range lines {
		if !known[line] {
			missing = append(missing, line)
		}
	}
	return missing
}

func compare(out io.Writer, found, recorded []string) int {
	added := missingFrom(found, recorded)
	stale := missingFrom(recorded, found)
	if len(added) == 0 && len(stale) == 0 {
		_, _ = fmt.Fprintf(out, "module-shape: %d violations, all in baseline\n", len(found))
		return 0
	}
	for _, line := range added {
		_, _ = fmt.Fprintf(out, "new: %s\n", line)
	}
	for _, line := range stale {
		_, _ = fmt.Fprintf(out, "fixed: remove it from the baseline: %s\n", line)
	}
	return 1
}
