package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"altune/go-api/internal/discovery/ports"
)

type BehavioralCorpusEntry struct {
	Query           string `json:"query"`
	ResultSignature string `json:"result_signature"`
	Title           string `json:"title"`
	Subtitle        string `json:"subtitle"`
	Polarity        int    `json:"polarity"`
}

type BehavioralCorpus struct {
	GeneratedFrom string                  `json:"generated_from"`
	Entries       []BehavioralCorpusEntry `json:"entries"`
}

func LoadBehavioralCorpus(path string) (BehavioralCorpus, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return BehavioralCorpus{}, fmt.Errorf("read behavioral corpus %q: %w", path, err)
	}
	var corpus BehavioralCorpus
	if err := json.Unmarshal(data, &corpus); err != nil {
		return BehavioralCorpus{}, fmt.Errorf("parse behavioral corpus %q: %w", path, err)
	}
	return corpus, nil
}

func (c BehavioralCorpus) Positives() []BehavioralCorpusEntry { return c.byPolarity(1) }
func (c BehavioralCorpus) Negatives() []BehavioralCorpusEntry { return c.byPolarity(-1) }

func (c BehavioralCorpus) byPolarity(sign int) []BehavioralCorpusEntry {
	out := []BehavioralCorpusEntry{}
	for _, e := range c.Entries {
		if (e.Polarity > 0) == (sign > 0) {
			out = append(out, e)
		}
	}
	return out
}

type CorpusBuilder struct {
	store ports.BehavioralLabelStore
}

func NewCorpusBuilder(store ports.BehavioralLabelStore) *CorpusBuilder {
	return &CorpusBuilder{store: store}
}

func (b *CorpusBuilder) Build(ctx context.Context, since time.Time, generatedFrom string) (BehavioralCorpus, error) {
	labels, err := b.store.BehavioralLabels(ctx, since)
	if err != nil {
		return BehavioralCorpus{}, fmt.Errorf("build behavioral corpus: %w", err)
	}
	entries := make([]BehavioralCorpusEntry, 0, len(labels))
	for _, l := range labels {
		entries = append(entries, BehavioralCorpusEntry{
			Query:           l.QueryNorm,
			ResultSignature: l.ResultSignature,
			Title:           l.Title,
			Subtitle:        l.Subtitle,
			Polarity:        l.Polarity,
		})
	}
	return BehavioralCorpus{GeneratedFrom: generatedFrom, Entries: entries}, nil
}

func (b *CorpusBuilder) Materialize(ctx context.Context, since time.Time, generatedFrom, path string) error {
	corpus, err := b.Build(ctx, since, generatedFrom)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(corpus, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal behavioral corpus: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write behavioral corpus %q: %w", path, err)
	}
	return nil
}
