package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

func vocabEntry(term string) domain.VocabularyEntry {
	return domain.VocabularyEntry{Term: term, TermNorm: term, Kind: domain.VocabKindArtist}
}

func suggestTerms(entries []domain.VocabularyEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Term
	}
	return out
}

func TestSuggest_PrefixFirstThenFuzzyTopUp(t *testing.T) {
	store := &fakeVocabularyStore{
		suggestByPrefixFn: func(_ string, _ int) ([]domain.VocabularyEntry, error) {
			return []domain.VocabularyEntry{vocabEntry("drake"), vocabEntry("drakeo")}, nil
		},
		findClosestFn: func(_ string, _ int) ([]domain.VocabularyEntry, error) {
			return []domain.VocabularyEntry{vocabEntry("dremke"), vocabEntry("droke")}, nil
		},
	}
	svc := NewSuggestService(store)

	got, err := svc.Execute(context.Background(), "drak", 4)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := []string{"drake", "drakeo", "dremke", "droke"}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, suggestTerms(got))
	}
	for i, term := range want {
		if got[i].Term != term {
			t.Fatalf("want prefix matches first then fuzzy: %v, got %v", want, suggestTerms(got))
		}
	}
}

func TestSuggest_FuzzyDuplicatesDeduped(t *testing.T) {
	store := &fakeVocabularyStore{
		suggestByPrefixFn: func(_ string, _ int) ([]domain.VocabularyEntry, error) {
			return []domain.VocabularyEntry{vocabEntry("drake")}, nil
		},
		findClosestFn: func(_ string, _ int) ([]domain.VocabularyEntry, error) {
			return []domain.VocabularyEntry{vocabEntry("drake"), vocabEntry("droke")}, nil
		},
	}
	svc := NewSuggestService(store)

	got, err := svc.Execute(context.Background(), "drak", 5)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := []string{"drake", "droke"}
	if len(got) != 2 || got[0].Term != want[0] || got[1].Term != want[1] {
		t.Fatalf("want deduped %v, got %v", want, suggestTerms(got))
	}
}

func TestSuggest_PrefixFillsLimitSkipsFuzzy(t *testing.T) {
	store := &fakeVocabularyStore{
		suggestByPrefixFn: func(_ string, _ int) ([]domain.VocabularyEntry, error) {
			return []domain.VocabularyEntry{vocabEntry("drake"), vocabEntry("drakeo")}, nil
		},
	}
	svc := NewSuggestService(store)

	got, err := svc.Execute(context.Background(), "drak", 2)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 results, got %v", suggestTerms(got))
	}
	if store.findClosestCalls != 0 {
		t.Errorf("fuzzy must not run when prefix fills the limit, got %d FindClosest calls", store.findClosestCalls)
	}
}

func TestSuggest_LimitCapsCombined(t *testing.T) {
	store := &fakeVocabularyStore{
		suggestByPrefixFn: func(_ string, _ int) ([]domain.VocabularyEntry, error) {
			return []domain.VocabularyEntry{vocabEntry("drake")}, nil
		},
		findClosestFn: func(_ string, _ int) ([]domain.VocabularyEntry, error) {
			return []domain.VocabularyEntry{vocabEntry("droke"), vocabEntry("dreke"), vocabEntry("druke")}, nil
		},
	}
	svc := NewSuggestService(store)

	got, err := svc.Execute(context.Background(), "drak", 2)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want limit=2 enforced, got %v", suggestTerms(got))
	}
}

func TestSuggest_EmptyQueryReturnsEmptyWithoutStoreCalls(t *testing.T) {
	store := &fakeVocabularyStore{}
	svc := NewSuggestService(store)

	got, err := svc.Execute(context.Background(), "  !!  ", 5)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty result for empty normalized query, got %v", suggestTerms(got))
	}
	if store.suggestCalls != 0 || store.findClosestCalls != 0 {
		t.Errorf("store must not be consulted for an empty query, got suggest=%d fuzzy=%d",
			store.suggestCalls, store.findClosestCalls)
	}
}

func TestSuggest_PrefixErrorPropagates(t *testing.T) {
	store := &fakeVocabularyStore{
		suggestByPrefixFn: func(_ string, _ int) ([]domain.VocabularyEntry, error) {
			return nil, errors.New("redis down")
		},
	}
	svc := NewSuggestService(store)

	if _, err := svc.Execute(context.Background(), "drak", 5); err == nil {
		t.Fatal("want the prefix lookup error propagated, got nil")
	}
}

func TestSuggest_FuzzyErrorDegradesToPrefixOnly(t *testing.T) {
	store := &fakeVocabularyStore{
		suggestByPrefixFn: func(_ string, _ int) ([]domain.VocabularyEntry, error) {
			return []domain.VocabularyEntry{vocabEntry("drake")}, nil
		},
		findClosestFn: func(_ string, _ int) ([]domain.VocabularyEntry, error) {
			return nil, errors.New("redis down")
		},
	}
	svc := NewSuggestService(store)

	got, err := svc.Execute(context.Background(), "drak", 5)
	if err != nil {
		t.Fatalf("fuzzy failure must be swallowed, got %v", err)
	}
	if len(got) != 1 || got[0].Term != "drake" {
		t.Fatalf("want the prefix results kept, got %v", suggestTerms(got))
	}
}

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
