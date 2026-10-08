package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var glossaryTerm = regexp.MustCompile(`\*\*([^*\n]+)\*\*|(?m)^#+\s+(.+)$`)

func checkConcepts(root string) ([]string, error) {
	repo := filepath.Join(root, "..", "..")
	terms, err := glossaryTerms(filepath.Join(repo, "docs", "ubiquitous-language.md"))
	if err != nil || terms == nil {
		return nil, err
	}
	modules, err := moduleNames(root)
	if err != nil {
		return nil, err
	}
	features, err := featureNames(filepath.Join(repo, "apps", "mobile", "src", "features"))
	return conceptLines(modules, features, terms), err
}

func conceptLines(modules, features []string, terms map[string]bool) []string {
	var lines []string
	for _, module := range modules {
		if !terms[squash(module)] {
			lines = append(lines, fmt.Sprintf("N2 internal/%s is not a term in docs/ubiquitous-language.md", module))
		}
		lines = append(lines, featureMismatches(module, features)...)
	}
	return lines
}

func glossaryTerms(path string) (map[string]bool, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	terms := map[string]bool{}
	for _, match := range glossaryTerm.FindAllStringSubmatch(string(content), -1) {
		terms[squash(match[1]+match[2])] = true
	}
	return terms, err
}

func squash(term string) string {
	var kept strings.Builder
	for _, char := range strings.ToLower(term) {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			kept.WriteRune(char)
		}
	}
	return kept.String()
}

func featureNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), "_") {
			names = append(names, entry.Name())
		}
	}
	return names, err
}

func featureMismatches(module string, features []string) []string {
	var lines []string
	for _, feature := range features {
		related := strings.HasPrefix(module, feature) || strings.HasPrefix(feature, module)
		if related && feature != module {
			lines = append(lines, fmt.Sprintf("N2 internal/%s differs from mobile feature %s", module, feature))
		}
	}
	return lines
}
