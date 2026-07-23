package service

import (
	"sync"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

func TestMaybeExplore_DisabledIsInert(t *testing.T) {
	svc := NewService(nil, NewCircuitBreaker()) // explorationRate 0
	in := []domain.SearchResult{{Title: "a"}, {Title: "b"}, {Title: "c"}}
	out, explored := svc.maybeExplore(in)
	if explored {
		t.Error("exploration must be off when rate is 0")
	}
	if &out[0] != &in[0] {
		t.Error("inert path must return the same slice, not a copy")
	}
}

func TestIngestVocabulary_UsesOrganicOrderNotExploredShuffle(t *testing.T) {
	// Vocabulary learning must ingest the organic ranked top, not the
	// exploration-shuffled slate — otherwise every explored search teaches the
	// vocabulary random tail results. Two identical services (one exploring at
	// rate 1.0) must ingest identical entries; repeated to make a shuffle leak
	// effectively impossible to miss.
	results := make([]domain.SearchResult, 0, 20)
	for i := 0; i < 20; i++ {
		results = append(results, deezerTrack("Humble Take "+string(rune('A'+i)), "Artist "+string(rune('A'+i)), float64(100-i)))
	}
	newSvc := func(store *fakeVocabularyStore, opts ...Option) *Service {
		p := &fakeProvider{name: domain.ProviderDeezer, results: results}
		opts = append([]Option{WithVocabularyStore(store)}, opts...)
		return NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), opts...)
	}
	capture := func(store *fakeVocabularyStore) *[]string {
		var mu sync.Mutex
		terms := &[]string{}
		store.addFn = func(e domain.VocabularyEntry) error {
			mu.Lock()
			defer mu.Unlock()
			*terms = append(*terms, e.Term)
			return nil
		}
		return terms
	}

	for run := 0; run < 5; run++ {
		organicStore, exploredStore := &fakeVocabularyStore{}, &fakeVocabularyStore{}
		organicTerms := capture(organicStore)
		exploredTerms := capture(exploredStore)

		organicSvc := newSvc(organicStore)
		exploredSvc := newSvc(exploredStore, WithExploration(1.0))

		runSearch(t, organicSvc, "humble")
		organicSvc.WaitForBackground()
		out := runSearch(t, exploredSvc, "humble")
		exploredSvc.WaitForBackground()

		if !out.Explored {
			t.Fatal("precondition: rate 1.0 must explore")
		}
		if len(*organicTerms) == 0 {
			t.Fatal("precondition: organic run ingested nothing")
		}
		if len(*organicTerms) != len(*exploredTerms) {
			t.Fatalf("run %d: ingest lengths differ: organic %v vs explored %v", run, *organicTerms, *exploredTerms)
		}
		for i := range *organicTerms {
			if (*organicTerms)[i] != (*exploredTerms)[i] {
				t.Fatalf("run %d: explored search ingested the shuffled slate, not the organic top:\norganic  %v\nexplored %v",
					run, *organicTerms, *exploredTerms)
			}
		}
	}
}

func TestMaybeExplore_AlwaysExploresClonesAndKeepsMembers(t *testing.T) {
	svc := NewService(nil, NewCircuitBreaker(), WithExploration(1.0)) // always explore
	in := []domain.SearchResult{{Title: "a"}, {Title: "b"}, {Title: "c"}}
	out, explored := svc.maybeExplore(in)

	if !explored {
		t.Fatal("rate 1.0 must always explore")
	}
	// Cache-safety: the input slice must be untouched (a shared cached list).
	if in[0].Title != "a" || in[1].Title != "b" || in[2].Title != "c" {
		t.Error("maybeExplore must not mutate the input (cache) slice")
	}
	// Same membership, just possibly reordered.
	seen := map[string]bool{}
	for _, r := range out {
		seen[r.Title] = true
	}
	if len(out) != 3 || !seen["a"] || !seen["b"] || !seen["c"] {
		t.Errorf("exploration must preserve membership, got %v", out)
	}
}
