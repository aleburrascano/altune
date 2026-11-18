package schema

import (
	"slices"
	"strings"
)

const (
	SeenReference = "reference"
	SeenLive      = "live"
	TypeUnseen    = "unseen"
)

type Schema struct {
	Provider  string             `yaml:"provider"`
	Reference string             `yaml:"reference"`
	Entities  map[string]*Entity `yaml:"entities"`
}

type Entity struct {
	Fixtures []Fixture `yaml:"fixtures"`
	Fields   []Field   `yaml:"fields"`
}

type Fixture struct {
	ID  string `yaml:"id"`
	Why string `yaml:"why"`
}

type Field struct {
	Path   string   `yaml:"path"`
	Type   string   `yaml:"type"`
	Seen   []string `yaml:"seen,flow"`
	Read   bool     `yaml:"read"`
	ByKey  bool     `yaml:"by_key,omitempty"`
	Unused string   `yaml:"unused,omitempty"`
	Absent string   `yaml:"absent,omitempty"`
}

func (s Schema) Sorted() Schema {
	for _, entity := range s.Entities {
		sortFields(entity.Fields)
	}
	return s
}

func sortFields(fields []Field) {
	slices.SortFunc(fields, func(a, b Field) int { return strings.Compare(a.Path, b.Path) })
}
