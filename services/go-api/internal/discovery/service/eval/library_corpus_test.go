package eval

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestNewLibraryCorpus_SortsForDeterminism(t *testing.T) {
	corpus := NewLibraryCorpus("2026-07-25", []LibraryEntity{
		{Title: "Zebra", Artist: "Beta"},
		{Title: "Alpha", Artist: "Beta"},
		{Title: "Anything", Artist: "Alpha"},
	})

	got := []string{}
	for _, e := range corpus.Entities {
		got = append(got, e.Artist+" — "+e.Title)
	}
	want := []string{"Alpha — Anything", "Beta — Alpha", "Beta — Zebra"}
	if !slices.Equal(got, want) {
		t.Errorf("entities = %v, want %v", got, want)
	}
}

func TestLibraryCorpus_SaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corpus-library.json")
	original := NewLibraryCorpus("2026-07-25", []LibraryEntity{
		{Title: "Hello", Artist: "Adele"},
		{Title: "Alright", Artist: "Kendrick Lamar"},
	})

	if err := original.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := LoadLibraryCorpus(path)
	if err != nil {
		t.Fatalf("LoadLibraryCorpus: %v", err)
	}
	if loaded.GeneratedAt != "2026-07-25" {
		t.Errorf("generated_at = %q, want 2026-07-25", loaded.GeneratedAt)
	}
	if !slices.Equal(loaded.Entities, original.Entities) {
		t.Errorf("entities = %v, want %v", loaded.Entities, original.Entities)
	}
}

func TestLoadLibraryCorpus_RejectsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corpus-library.json")
	if err := os.WriteFile(path, []byte(`{"generated_at":"x","entities":[]}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadLibraryCorpus(path); err == nil {
		t.Fatal("expected an error for an empty corpus, got nil")
	}
}

func TestLibraryCorpus_ArtistsAndTermsAreDistinctAndSorted(t *testing.T) {
	corpus := NewLibraryCorpus("2026-07-25", []LibraryEntity{
		{Title: "Alright", Artist: "Kendrick Lamar"},
		{Title: "DNA.", Artist: "Kendrick Lamar"},
		{Title: "Hello", Artist: "Adele"},
		{Title: "Hello", Artist: ""},
	})

	if got, want := corpus.Artists(), []string{"Adele", "Kendrick Lamar"}; !slices.Equal(got, want) {
		t.Errorf("Artists() = %v, want %v", got, want)
	}
	want := []string{"Adele", "Alright", "DNA.", "Hello", "Kendrick Lamar"}
	if got := corpus.Terms(); !slices.Equal(got, want) {
		t.Errorf("Terms() = %v, want %v", got, want)
	}
}
