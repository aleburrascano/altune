package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"strings"
	"sync"
	"testing"
)

type sharedVocabulary struct {
	mu      sync.Mutex
	entries []domain.VocabularyEntry
}

func (v *sharedVocabulary) store() *fakeVocabularyStore {
	return &fakeVocabularyStore{
		addFn: func(e domain.VocabularyEntry) error {
			v.mu.Lock()
			defer v.mu.Unlock()
			v.entries = append(v.entries, e)
			return nil
		},
		suggestByPrefixFn: func(prefix string, limit int) ([]domain.VocabularyEntry, error) {
			v.mu.Lock()
			defer v.mu.Unlock()
			var matches []domain.VocabularyEntry
			for _, e := range v.entries {
				if strings.HasPrefix(e.TermNorm, prefix) && len(matches) < limit {
					matches = append(matches, e)
				}
			}
			return matches, nil
		},
	}
}

func suggestedNorms(t *testing.T, suggest *SuggestService, partial string) []string {
	t.Helper()
	entries, err := suggest.Execute(context.Background(), partial, 10)
	if err != nil {
		t.Fatalf("suggest: %v", err)
	}
	norms := make([]string, 0, len(entries))
	for _, e := range entries {
		norms = append(norms, e.TermNorm)
	}
	return norms
}

func TestSearch_RawQueryIsNeverSuggestedToAnotherUser(t *testing.T) {
	const private = "jane doe 42 elm street"
	vocab := &sharedVocabulary{}
	store := vocab.store()
	p := &queryFakeProvider{
		name: domain.ProviderDeezer,
		resultsByQuery: map[string][]domain.SearchResult{
			private: {deezerTrack("Elm Street", "Jane Band", 60)},
		},
	}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithVocabularyStore(store))

	runSearch(t, svc, private)
	svc.WaitForBackground()

	suggest := NewSuggestService(store)
	for _, norm := range suggestedNorms(t, suggest, "jane") {
		if strings.Contains(norm, "doe") {
			t.Fatalf("another user's raw query leaked into suggestions: %q", norm)
		}
	}
	if got := suggestedNorms(t, suggest, "elm"); len(got) == 0 {
		t.Fatalf("want the provider-verified title still suggested, got none")
	}
}
