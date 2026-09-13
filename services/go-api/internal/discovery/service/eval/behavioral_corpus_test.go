package eval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"altune/go-api/internal/discovery/ports"
)

type fakeLabelStore struct {
	labels []ports.BehavioralLabel
}

func (f fakeLabelStore) BehavioralLabels(context.Context, time.Time) ([]ports.BehavioralLabel, error) {
	return f.labels, nil
}

func TestCorpusBuilder_BuildMapsLabels(t *testing.T) {
	store := fakeLabelStore{labels: []ports.BehavioralLabel{
		{QueryNorm: "adele hello", ResultSignature: "track|hello|adele", Title: "Hello", Subtitle: "Adele", Polarity: 1},
		{QueryNorm: "wrong one", ResultSignature: "album|x|y", Title: "X", Subtitle: "Y", Polarity: -1},
	}}
	builder := NewCorpusBuilder(store)

	corpus, err := builder.Build(context.Background(), time.Unix(0, 0), "2026-06-01")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if corpus.GeneratedFrom != "2026-06-01" {
		t.Errorf("generated_from = %q, want 2026-06-01", corpus.GeneratedFrom)
	}
	if len(corpus.Positives()) != 1 || corpus.Positives()[0].Query != "adele hello" {
		t.Errorf("positives = %v", corpus.Positives())
	}
	if len(corpus.Negatives()) != 1 || corpus.Negatives()[0].ResultSignature != "album|x|y" {
		t.Errorf("negatives = %v", corpus.Negatives())
	}
}

func TestCorpusBuilder_MaterializeRoundTrip(t *testing.T) {
	store := fakeLabelStore{labels: []ports.BehavioralLabel{
		{QueryNorm: "q", ResultSignature: "s", Title: "T", Subtitle: "Sub", Polarity: 1},
	}}
	builder := NewCorpusBuilder(store)
	path := filepath.Join(t.TempDir(), "behavioral_corpus.json")

	if err := builder.Materialize(context.Background(), time.Unix(0, 0), "win", path); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	loaded, err := LoadBehavioralCorpus(path)
	if err != nil {
		t.Fatalf("LoadBehavioralCorpus: %v", err)
	}
	if len(loaded.Entries) != 1 || loaded.Entries[0].ResultSignature != "s" {
		t.Errorf("round-trip mismatch: %+v", loaded)
	}
}

func TestLoadBehavioralCorpus_MissingFile(t *testing.T) {
	if _, err := LoadBehavioralCorpus(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Fatal("want an error for a missing corpus file")
	}
}

func TestLoadBehavioralCorpus_BadJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBehavioralCorpus(path); err == nil {
		t.Fatal("want a parse error for malformed JSON")
	}
}

type erroringLabelStore struct{}

func (erroringLabelStore) BehavioralLabels(context.Context, time.Time) ([]ports.BehavioralLabel, error) {
	return nil, errors.New("db down")
}

func TestCorpusBuilder_BuildAndMaterializePropagateStoreError(t *testing.T) {
	builder := NewCorpusBuilder(erroringLabelStore{})
	if _, err := builder.Build(context.Background(), time.Unix(0, 0), "x"); err == nil {
		t.Error("Build must propagate the label-store error")
	}
	path := filepath.Join(t.TempDir(), "out.json")
	if err := builder.Materialize(context.Background(), time.Unix(0, 0), "x", path); err == nil {
		t.Error("Materialize must propagate the build error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("no corpus file may be written when the build failed")
	}
}

func TestCorpusBuilder_MaterializeUnwritablePath(t *testing.T) {
	store := fakeLabelStore{}
	builder := NewCorpusBuilder(store)
	path := filepath.Join(t.TempDir(), "no-such-dir", "corpus.json")
	if err := builder.Materialize(context.Background(), time.Unix(0, 0), "x", path); err == nil {
		t.Error("Materialize must surface the write error")
	}
}
