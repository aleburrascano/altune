package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"altune/go-api/internal/discovery/ports"
)

// BehavioralCorpusEntry is one behavior-mined relevance label in the eval corpus
// format: the query the user typed, the result their behavior labelled, and the
// polarity (+1 the behavior proved relevant — a completed/library_add; −1 a hard
// negative — a wrong_album). The self-growing corpus is a slice of these.
type BehavioralCorpusEntry struct {
	Query           string `json:"query"`
	ResultSignature string `json:"result_signature"`
	Title           string `json:"title"`
	Subtitle        string `json:"subtitle"`
	Polarity        int    `json:"polarity"`
}

// BehavioralCorpus is the materialized self-growing eval corpus. Generated nightly
// from behavioral labels; it sharpens with use because the labels are about the
// user's own catalog and tastes — the in-sample answer to why a global popularity
// signal failed on this niche library.
type BehavioralCorpus struct {
	GeneratedFrom string                  `json:"generated_from"`
	Entries       []BehavioralCorpusEntry `json:"entries"`
}

// LoadBehavioralCorpus reads a materialized behavioral corpus from disk. Used by
// the offline counterfactual replay to score a candidate ranker against labels
// the system mined itself.
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

// Positives / Negatives split the corpus by polarity for the replay scorer.
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

// CorpusBuilder mines the self-growing eval corpus from behavioral labels
// (search→completed/library_add ⇒ positive; wrong_album ⇒ hard negative). Lives
// in the eval package (not the application layer) because materializing a corpus
// to disk is offline-harness tooling, and because `service` cannot import `eval`.
type CorpusBuilder struct {
	store ports.BehavioralLabelStore
}

func NewCorpusBuilder(store ports.BehavioralLabelStore) *CorpusBuilder {
	return &CorpusBuilder{store: store}
}

// Build reads the behavioral labels since `since` and shapes them into the
// corpus format. `generatedFrom` is stamped verbatim onto the corpus.
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

// Materialize builds the corpus and writes it to path as indented JSON — the
// nightly job's persistence step.
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
