package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

type LibraryCorpus struct {
	GeneratedAt string          `json:"generated_at"`
	Entities    []LibraryEntity `json:"entities"`
}

func LoadLibraryCorpus(path string) (LibraryCorpus, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return LibraryCorpus{}, fmt.Errorf("read library corpus %q: %w", path, err)
	}
	var corpus LibraryCorpus
	if err := json.Unmarshal(data, &corpus); err != nil {
		return LibraryCorpus{}, fmt.Errorf("parse library corpus %q: %w", path, err)
	}
	if len(corpus.Entities) == 0 {
		return LibraryCorpus{}, fmt.Errorf("library corpus %q has no entities", path)
	}
	return corpus, nil
}

func NewLibraryCorpus(generatedAt string, entities []LibraryEntity) LibraryCorpus {
	sorted := append([]LibraryEntity{}, entities...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Artist != sorted[j].Artist {
			return sorted[i].Artist < sorted[j].Artist
		}
		return sorted[i].Title < sorted[j].Title
	})
	return LibraryCorpus{GeneratedAt: generatedAt, Entities: sorted}
}

func (c LibraryCorpus) Save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal library corpus: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write library corpus %q: %w", path, err)
	}
	return nil
}

func (c LibraryCorpus) Artists() []string {
	return distinctSorted(func(yield func(string)) {
		for _, e := range c.Entities {
			yield(e.Artist)
		}
	})
}

func (c LibraryCorpus) Terms() []string {
	return distinctSorted(func(yield func(string)) {
		for _, e := range c.Entities {
			yield(e.Artist)
			yield(e.Title)
		}
	})
}

func distinctSorted(each func(yield func(string))) []string {
	seen := map[string]struct{}{}
	out := []string{}
	each(func(s string) {
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	})
	sort.Strings(out)
	return out
}
