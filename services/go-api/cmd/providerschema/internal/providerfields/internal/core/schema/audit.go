package schema

import (
	"fmt"
	"slices"
)

func (e *Entity) Account(reads Reads) {
	known := map[string]bool{}
	for i := range e.Fields {
		match := reads.Match(e.Fields[i].Path)
		e.Fields[i].Read, e.Fields[i].ByKey = match.Read, match.ByKey
		known[e.Fields[i].Path] = true
	}
	for path := range reads.Paths {
		if !known[path] {
			e.Fields = append(e.Fields, Field{Path: path, Type: TypeUnseen, Read: true})
		}
	}
	sortFields(e.Fields)
}

func (e *Entity) Audit(name string, reads Reads) []string {
	var problems []string
	known := map[string]bool{}
	for _, field := range e.Fields {
		known[field.Path] = true
		problems = append(problems, fieldProblems(name, field, reads.Match(field.Path))...)
	}
	for path := range reads.Paths {
		if !known[path] {
			problems = append(problems, fmt.Sprintf("%s.%s: the adapter decodes it but the schema does not list it", name, path))
		}
	}
	slices.Sort(problems)
	return problems
}

type fieldRule struct {
	broken  func(field Field, match Match) bool
	problem string
}

var fieldRules = []fieldRule{
	{func(f Field, m Match) bool { return len(f.Seen) == 0 && !m.Read }, "neither the reference nor a live probe shows it any more"},
	{func(f Field, m Match) bool { return f.Read != m.Read || f.ByKey != m.ByKey }, "the read flag disagrees with the adapter (run: go test ./cmd/providerschema -update-schema)"},
	{func(f Field, _ Match) bool { return len(f.Seen) == 0 && f.Absent == "" }, "the adapter decodes it but this response never sends it, so write an absent reason or stop decoding it"},
	{func(f Field, _ Match) bool { return len(f.Seen) > 0 && f.Absent != "" }, "the response sends it now, so drop its absent reason"},
	{func(f Field, m Match) bool { return !m.Read && f.Unused == "" }, "the adapter does not read it and no unused reason is written"},
	{func(f Field, m Match) bool { return m.Read && !m.ByKey && f.Unused != "" }, "the adapter reads it, so drop its unused reason"},
}

func fieldProblems(entity string, field Field, match Match) []string {
	for _, rule := range fieldRules {
		if rule.broken(field, match) {
			return []string{fmt.Sprintf("%s.%s: %s", entity, field.Path, rule.problem)}
		}
	}
	return nil
}
