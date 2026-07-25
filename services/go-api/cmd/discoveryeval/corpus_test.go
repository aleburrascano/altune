package main

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	discoveryEval "altune/go-api/internal/discovery/service/eval"
)

func writeTestCorpus(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "corpus-library.json")
	corpus := discoveryEval.NewLibraryCorpus("2026-07-25", []discoveryEval.LibraryEntity{
		{Artist: "Kendrick Lamar", Title: "Alright"},
		{Artist: "Adele", Title: "Hello"},
		{Artist: "Kendrick Lamar", Title: "DNA."},
	})
	if err := corpus.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return path
}

func TestResolveEntities_FrozenCorpusIsDeterministic(t *testing.T) {
	opts := options{corpusFile: writeTestCorpus(t)}

	first, err := resolveEntities(context.Background(), nil, opts)
	if err != nil {
		t.Fatalf("resolveEntities: %v", err)
	}
	second, err := resolveEntities(context.Background(), nil, opts)
	if err != nil {
		t.Fatalf("resolveEntities: %v", err)
	}
	if !slices.Equal(first, second) {
		t.Errorf("two reads of the same snapshot differ:\n%v\n%v", first, second)
	}
	if len(first) != 3 {
		t.Errorf("got %d entities, want 3", len(first))
	}
}

func TestResolveEntities_FrozenCorpusHonoursLimit(t *testing.T) {
	opts := options{corpusFile: writeTestCorpus(t), limit: 2}

	got, err := resolveEntities(context.Background(), nil, opts)
	if err != nil {
		t.Fatalf("resolveEntities: %v", err)
	}
	if len(got) != 2 || got[0].Artist != "Adele" {
		t.Errorf("limited corpus = %v, want the first 2 in sorted order", got)
	}
}

func TestResolveArtistsAndTerms_DeriveFromTheSameSnapshot(t *testing.T) {
	opts := options{corpusFile: writeTestCorpus(t)}

	artists, err := resolveArtists(context.Background(), nil, opts, 0)
	if err != nil {
		t.Fatalf("resolveArtists: %v", err)
	}
	if want := []string{"Adele", "Kendrick Lamar"}; !slices.Equal(artists, want) {
		t.Errorf("artists = %v, want %v", artists, want)
	}

	terms, err := resolveTerms(context.Background(), nil, opts)
	if err != nil {
		t.Fatalf("resolveTerms: %v", err)
	}
	if len(terms) != 5 {
		t.Errorf("terms = %v, want 5 distinct", terms)
	}
}

func TestLoadFrozenCorpus_RejectsRandom(t *testing.T) {
	opts := options{corpusFile: writeTestCorpus(t), random: true}

	if _, err := resolveEntities(context.Background(), nil, opts); err == nil {
		t.Fatal("expected -random with -corpus-file to be rejected")
	}
}
