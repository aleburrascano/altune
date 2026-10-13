package main

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const capabilityFileCap = 16

var packageName = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

var capabilityLayers = []string{"service", "adapters"}

func checkCapabilities(root string) ([]string, error) {
	modules, err := moduleNames(root)
	if err != nil {
		return nil, err
	}
	var found []string
	for _, module := range modules {
		for _, layer := range capabilityLayers {
			lines, err := checkCapabilityLayer(root, module, layer)
			if err != nil {
				return nil, err
			}
			found = append(found, lines...)
		}
	}
	return found, nil
}

func checkCapabilityLayer(root, module, layer string) ([]string, error) {
	top := filepath.Join(root, "internal", module, layer)
	var found []string
	err := filepath.WalkDir(top, func(dir string, entry fs.DirEntry, err error) error {
		if err != nil {
			if dir == top && os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if dir != top && ignoredFolder(entry.Name()) {
			return fs.SkipDir
		}
		lines, err := checkCapabilityDir(root, module, layer, dir, dir == top)
		found = append(found, lines...)
		return err
	})
	return found, err
}

func ignoredFolder(name string) bool {
	return strings.HasPrefix(name, "_") || skipEntry(name)
}

func checkCapabilityDir(root, module, layer, dir string, isLayer bool) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return nil, err
	}
	rel = filepath.ToSlash(rel)
	sources, packages := splitEntries(entries)
	lines := badPackageLines(module, rel, packages)
	hasPackages, err := anyHoldsGo(dir, packages)
	if err != nil {
		return nil, err
	}
	lines = append(lines, sourceLines(rel, layer, sources, isLayer, hasPackages)...)
	if !isLayer && len(sources) > capabilityFileCap {
		lines = append(lines, fmt.Sprintf("G8 %s holds more than %d files", rel, capabilityFileCap))
	}
	return lines, nil
}

func badPackageLines(module, rel string, packages []string) []string {
	var lines []string
	for _, name := range packages {
		if badPackageName(module, name) {
			lines = append(lines, fmt.Sprintf("G8 %s bad package name", path.Join(rel, name)))
		}
	}
	return lines
}

func splitEntries(entries []fs.DirEntry) (sources, packages []string) {
	for _, entry := range entries {
		name := entry.Name()
		switch {
		case !entry.IsDir() && isSourceFile(name):
			sources = append(sources, name)
		case entry.IsDir() && !ignoredFolder(name):
			packages = append(packages, name)
		}
	}
	return sources, packages
}

func anyHoldsGo(dir string, packages []string) (bool, error) {
	for _, name := range packages {
		held, err := hasGoFiles(filepath.Join(dir, name))
		if err != nil {
			return false, err
		}
		if held {
			return true, nil
		}
	}
	return false, nil
}

func sourceLines(rel, layer string, sources []string, isLayer, hasPackages bool) []string {
	var lines []string
	for _, name := range sources {
		switch {
		case isLayer:
			lines = append(lines, fmt.Sprintf("G8 %s sits directly in %s/", path.Join(rel, name), layer))
		case hasPackages:
			lines = append(lines, fmt.Sprintf("G8 %s sits beside packages", path.Join(rel, name)))
		}
	}
	return lines
}

func badPackageName(module, name string) bool {
	return !packageName.MatchString(name) || name == module || slices.Contains(layers, name)
}

func hasGoFiles(dir string) (bool, error) {
	found := false
	err := filepath.WalkDir(dir, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && p != dir && ignoredFolder(entry.Name()) {
			return fs.SkipDir
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found, err
}
