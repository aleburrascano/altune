package schema

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

type Reads struct {
	Paths  map[string]bool
	Opaque []string
	Keys   map[string]bool
	Maps   []string
}

type Match struct {
	Read  bool
	ByKey bool
}

func (r Reads) Has(path string) bool {
	return r.Match(path).Read
}

func (r Reads) Match(path string) Match {
	if r.Paths[path] {
		return Match{Read: true}
	}
	byKey := r.Keys[lastKey(path)] && slices.ContainsFunc(r.Opaque, func(root string) bool { return within(path, root) })
	return Match{Read: byKey, ByKey: byKey}
}

func within(path, root string) bool {
	return root == "" || strings.HasPrefix(path, root+".") || strings.HasPrefix(path, root+"[]")
}

func lastKey(path string) string {
	trimmed := strings.TrimSuffix(path, "[]")
	return trimmed[strings.LastIndex(trimmed, ".")+1:]
}

func (r Reads) Graft(at string, sub Reads) (Reads, error) {
	i := slices.Index(r.Opaque, at)
	if i < 0 {
		return Reads{}, fmt.Errorf("%s is not a raw field the adapter keeps to decode later", at)
	}
	out := Reads{
		Paths:  maps.Clone(r.Paths),
		Opaque: slices.Delete(slices.Clone(r.Opaque), i, i+1),
		Keys:   map[string]bool{},
		Maps:   slices.Clone(r.Maps),
	}
	maps.Copy(out.Keys, r.Keys)
	maps.Copy(out.Keys, sub.Keys)
	for path := range sub.Paths {
		out.Paths[joinPath(at, path)] = true
	}
	out.Opaque = append(out.Opaque, prefixed(at, sub.Opaque)...)
	out.Maps = append(out.Maps, prefixed(at, sub.Maps)...)
	return out, nil
}

func prefixed(at string, paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		out = append(out, joinPath(at, path))
	}
	return out
}

func joinPath(at, path string) string {
	if path == "" {
		return at
	}
	return at + "." + path
}
