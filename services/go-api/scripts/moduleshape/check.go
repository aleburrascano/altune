package main

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	modulePath     = "altune/go-api"
	internalPrefix = modulePath + "/internal/"
	bridgeSuffix   = "bridge"
)

var layers = []string{"domain", "ports", "service", "adapters"}

var inwardLayers = map[string][]string{
	"domain":  {"domain"},
	"ports":   {"domain", "ports"},
	"service": {"domain", "ports", "service"},
}

var bannedStems = []string{
	"util", "utils", "helper", "helpers", "common", "misc", "lib", "core",
	"manager", "base", "generic", "data", "stuff", "tmp", "temp", "new",
	"old", "types", "constants",
}

var skippedNames = []string{"vendor", "node_modules", "testdata"}

type importRef struct {
	file   string
	module string
	layer  string
	path   string
	line   int
}

func collect(root string) ([]string, error) {
	var found []string
	modules, err := moduleNames(root)
	if err != nil {
		return nil, err
	}
	for _, module := range modules {
		lines, err := checkModule(root, module)
		if err != nil {
			return nil, err
		}
		found = append(found, lines...)
	}
	names, err := checkNames(root)
	if err != nil {
		return nil, err
	}
	return normalize(append(found, names...)), nil
}

func moduleNames(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "internal"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var modules []string
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != "app" && entry.Name() != "shared" {
			modules = append(modules, entry.Name())
		}
	}
	return modules, nil
}

func checkModule(root, module string) ([]string, error) {
	lines, err := checkRoot(root, module)
	if err != nil {
		return nil, err
	}
	refs, err := moduleImports(root, module)
	if err != nil {
		return nil, err
	}
	lines = append(lines, checkLayers(refs)...)
	lines = append(lines, checkCrossModule(refs)...)
	bridges, err := checkBridgePackages(root, module, refs)
	return append(lines, bridges...), err
}

func checkRoot(root, module string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "internal", module))
	if err != nil {
		return nil, err
	}
	present := map[string]bool{}
	var lines []string
	for _, entry := range entries {
		if allowedEntry(module, entry) {
			present[entry.Name()] = true
			continue
		}
		lines = append(lines, fmt.Sprintf("G1 internal/%s/%s unexpected root entry", module, entry.Name()))
	}
	return append(lines, missingEntries(module, present)...), nil
}

func allowedEntry(module string, entry fs.DirEntry) bool {
	if entry.IsDir() {
		return slices.Contains(layers, entry.Name()) || entry.Name() == module+"test"
	}
	return entry.Name() == "ARCHITECTURE.md"
}

func missingEntries(module string, present map[string]bool) []string {
	var lines []string
	for _, layer := range layers {
		if !present[layer] {
			lines = append(lines, fmt.Sprintf("G1 internal/%s missing %s/", module, layer))
		}
	}
	if !present["ARCHITECTURE.md"] {
		lines = append(lines, fmt.Sprintf("G1 internal/%s missing ARCHITECTURE.md", module))
	}
	return lines
}

func moduleImports(root, module string) ([]importRef, error) {
	var refs []importRef
	fset := token.NewFileSet()
	err := filepath.WalkDir(filepath.Join(root, "internal", module), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !isSourceFile(entry.Name()) {
			return err
		}
		found, err := fileImports(fset, root, module, path)
		refs = append(refs, found...)
		return err
	})
	return refs, err
}

func isSourceFile(name string) bool {
	return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
}

func fileImports(fset *token.FileSet, root, module, path string) ([]importRef, error) {
	parsed, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return nil, err
	}
	rel = filepath.ToSlash(rel)
	var refs []importRef
	for _, spec := range parsed.Imports {
		target, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return nil, err
		}
		refs = append(refs, importRef{rel, module, layerOf(rel, module), target, fset.Position(spec.Pos()).Line})
	}
	return refs, nil
}

func layerOf(rel, module string) string {
	inside := strings.TrimPrefix(rel, "internal/"+module+"/")
	layer, _, nested := strings.Cut(inside, "/")
	if !nested {
		return ""
	}
	return layer
}

func checkLayers(refs []importRef) []string {
	var lines []string
	for _, ref := range refs {
		if !importAllowed(ref) {
			lines = append(lines, fmt.Sprintf("G2 %s:%d %s imports %s", ref.file, ref.line, ref.layer, ref.path))
		}
	}
	return lines
}

func importAllowed(ref importRef) bool {
	allowed, restricted := inwardLayers[ref.layer]
	if !restricted {
		return true
	}
	rest, internal := strings.CutPrefix(ref.path, internalPrefix)
	if !internal {
		return isStdlib(ref.path)
	}
	owner, sub, _ := strings.Cut(rest, "/")
	if _, cross := crossModule(ref); cross || owner == "shared" {
		return true
	}
	sublayer, _, _ := strings.Cut(sub, "/")
	return owner == ref.module && slices.Contains(allowed, sublayer)
}

func isStdlib(path string) bool {
	if path == modulePath || strings.HasPrefix(path, modulePath+"/") {
		return false
	}
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

func crossModule(ref importRef) (string, bool) {
	rest, internal := strings.CutPrefix(ref.path, internalPrefix)
	if !internal {
		return "", false
	}
	owner, _, _ := strings.Cut(rest, "/")
	other := owner != "" && owner != ref.module && owner != "app" && owner != "shared"
	return owner, other
}

func checkCrossModule(refs []importRef) []string {
	var lines []string
	for _, ref := range refs {
		target, cross := crossModule(ref)
		bridgeDir := fmt.Sprintf("internal/%s/adapters/%s%s", ref.module, target, bridgeSuffix)
		if cross && !strings.HasPrefix(ref.file, bridgeDir+"/") {
			lines = append(lines, fmt.Sprintf("G3 %s:%d imports %s outside adapters/%s%s", ref.file, ref.line, target, target, bridgeSuffix))
		}
	}
	return lines
}

func checkBridgePackages(root, module string, refs []importRef) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "internal", module, "adapters"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, entry := range entries {
		target, isBridge := strings.CutSuffix(entry.Name(), bridgeSuffix)
		dir := fmt.Sprintf("internal/%s/adapters/%s", module, entry.Name())
		if entry.IsDir() && isBridge && target != "" && !reachesTarget(refs, dir, target) {
			lines = append(lines, fmt.Sprintf("G3 %s imports nothing from %s ports or service", dir, target))
		}
	}
	return lines, nil
}

func reachesTarget(refs []importRef, dir, target string) bool {
	for _, ref := range refs {
		if strings.HasPrefix(ref.file, dir+"/") && importsLayer(ref.path, target, "ports", "service") {
			return true
		}
	}
	return false
}

func importsLayer(path, target string, wanted ...string) bool {
	for _, layer := range wanted {
		prefix := internalPrefix + target + "/" + layer
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func checkNames(root string) ([]string, error) {
	var lines []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || path == root {
			return err
		}
		if skipEntry(entry.Name()) {
			return skipResult(entry)
		}
		if stem, banned := bannedStem(path, entry); banned {
			rel, err := filepath.Rel(root, path)
			lines = append(lines, fmt.Sprintf("N1 %s banned name %s", filepath.ToSlash(rel), stem))
			return err
		}
		return nil
	})
	return lines, err
}

func skipEntry(name string) bool {
	return strings.HasPrefix(name, ".") || slices.Contains(skippedNames, name)
}

func skipResult(entry fs.DirEntry) error {
	if entry.IsDir() {
		return fs.SkipDir
	}
	return nil
}

func bannedStem(path string, entry fs.DirEntry) (string, bool) {
	stem, _, _ := strings.Cut(entry.Name(), ".")
	stem = strings.TrimSuffix(stem, "_test")
	if !slices.Contains(bannedStems, stem) {
		return "", false
	}
	if strings.HasSuffix(entry.Name(), "_test.go") {
		_, err := os.Stat(filepath.Join(filepath.Dir(path), stem+".go"))
		return stem, err != nil
	}
	return stem, true
}
