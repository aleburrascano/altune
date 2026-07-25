package service

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

type queryFakeProvider struct {
	name           domain.ProviderName
	resultsByQuery map[string][]domain.SearchResult
}

func (p *queryFakeProvider) Name() domain.ProviderName { return p.name }

func (p *queryFakeProvider) Search(_ context.Context, query string, _ map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	return p.resultsByQuery[query], nil
}

func (p *queryFakeProvider) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

func humbleVocab() *fakeVocabularyStore {
	return &fakeVocabularyStore{
		findClosestFn: func(_ string, _ int) ([]domain.VocabularyEntry, error) {
			return []domain.VocabularyEntry{
				{Term: "humble", TermNorm: "humble", Kind: domain.VocabKindTrack, MatchScore: 0.8},
			}, nil
		},
	}
}

func TestService_Execute_ZeroResultsTriggersCorrection(t *testing.T) {
	p := &queryFakeProvider{
		name: domain.ProviderDeezer,
		resultsByQuery: map[string][]domain.SearchResult{
			"humble": {deezerTrack("HUMBLE.", "Kendrick Lamar", 80)},
		},
	}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithVocabularyStore(humbleVocab()))

	out := runSearch(t, svc, "humbel")
	svc.WaitForBackground()

	if out.CorrectedQuery != "humble" {
		t.Errorf("want CorrectedQuery=%q, got %q", "humble", out.CorrectedQuery)
	}
	if out.OriginalQuery != "humbel" {
		t.Errorf("want OriginalQuery=%q, got %q", "humbel", out.OriginalQuery)
	}
	if len(out.Results) != 1 || out.Results[0].Title != "HUMBLE." {
		t.Fatalf("want the corrected search's result, got %v", titles(out.Results))
	}
}

func TestService_Execute_CorrectedResultsNeverCached(t *testing.T) {
	p := &queryFakeProvider{
		name: domain.ProviderDeezer,
		resultsByQuery: map[string][]domain.SearchResult{
			"humble": {deezerTrack("HUMBLE.", "Kendrick Lamar", 80)},
		},
	}
	cache := newFakeResultCache()
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(),
		WithVocabularyStore(humbleVocab()), WithResultCache(cache))

	out := runSearch(t, svc, "humbel")
	svc.WaitForBackground()

	if out.CorrectedQuery != "humble" {
		t.Fatalf("precondition: correction must fire, got %q", out.CorrectedQuery)
	}
	if cache.sets != 0 {
		t.Errorf("corrected results must never be cached, sets = %d (store: %v)", cache.sets, cache.store)
	}
}

type erroringThenFakeProvider struct {
	name    domain.ProviderName
	failFor string
	results map[string][]domain.SearchResult
}

func (p *erroringThenFakeProvider) Name() domain.ProviderName { return p.name }

func (p *erroringThenFakeProvider) Search(_ context.Context, query string, _ map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	if query == p.failFor {
		return nil, errors.New("boom")
	}
	return p.results[query], nil
}

func (p *erroringThenFakeProvider) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

func TestService_Execute_CorrectionUsesCorrectedFanOutStatuses(t *testing.T) {
	p := &erroringThenFakeProvider{
		name:    domain.ProviderDeezer,
		failFor: "humbel",
		results: map[string][]domain.SearchResult{
			"humble": {deezerTrack("HUMBLE.", "Kendrick Lamar", 80)},
		},
	}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithVocabularyStore(humbleVocab()))

	out := runSearch(t, svc, "humbel")
	svc.WaitForBackground()

	if out.CorrectedQuery != "humble" {
		t.Fatalf("precondition: correction must fire, got %q", out.CorrectedQuery)
	}
	if len(out.ProviderStatuses) != 1 || out.ProviderStatuses[0].Status != domain.ProviderStatusOK {
		t.Fatalf("want the corrected fan-out's OK status, got %+v", out.ProviderStatuses)
	}
	if out.ProviderStatuses[0].ResultCount != 1 {
		t.Errorf("want the corrected fan-out's result count 1, got %d", out.ProviderStatuses[0].ResultCount)
	}
	if out.Partial {
		t.Error("partial must reflect the corrected (complete) run, not the failed original")
	}
}

func TestService_Execute_ResultsDoNotTriggerCorrection(t *testing.T) {
	store := humbleVocab()
	p := &queryFakeProvider{
		name: domain.ProviderDeezer,
		resultsByQuery: map[string][]domain.SearchResult{
			"humble": {deezerTrack("HUMBLE.", "Kendrick Lamar", 80)},
		},
	}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithVocabularyStore(store))

	out := runSearch(t, svc, "humble")
	svc.WaitForBackground()

	if out.CorrectedQuery != "" || out.OriginalQuery != "" {
		t.Errorf("want no correction, got corrected=%q original=%q", out.CorrectedQuery, out.OriginalQuery)
	}
	if len(out.Results) != 1 {
		t.Fatalf("want the direct result, got %v", titles(out.Results))
	}
	if store.findClosestCalls != 0 {
		t.Errorf("correction must not run when the search has results, got %d FindClosest calls", store.findClosestCalls)
	}
}

func TestService_Execute_NoCorrectionCandidateReturnsEmpty(t *testing.T) {
	store := &fakeVocabularyStore{}
	p := &queryFakeProvider{name: domain.ProviderDeezer}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithVocabularyStore(store))

	out := runSearch(t, svc, "humbel")
	svc.WaitForBackground()

	if len(out.Results) != 0 {
		t.Fatalf("want zero results, got %v", titles(out.Results))
	}
	if out.CorrectedQuery != "" || out.OriginalQuery != "" {
		t.Errorf("want no correction fields, got corrected=%q original=%q", out.CorrectedQuery, out.OriginalQuery)
	}
}

func TestService_Execute_CorrectedSearchAlsoEmptyReturnsEmpty(t *testing.T) {
	p := &queryFakeProvider{name: domain.ProviderDeezer}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithVocabularyStore(humbleVocab()))

	out := runSearch(t, svc, "humbel")
	svc.WaitForBackground()

	if len(out.Results) != 0 {
		t.Fatalf("want zero results, got %v", titles(out.Results))
	}
	if out.CorrectedQuery != "" || out.OriginalQuery != "" {
		t.Errorf("a fruitless correction must not be surfaced, got corrected=%q original=%q", out.CorrectedQuery, out.OriginalQuery)
	}
}

func TestService_Execute_ExactVocabMatchNotCorrected(t *testing.T) {
	store := &fakeVocabularyStore{
		findClosestFn: func(_ string, _ int) ([]domain.VocabularyEntry, error) {
			return []domain.VocabularyEntry{
				{Term: "humble", TermNorm: "humble", Kind: domain.VocabKindTrack, MatchScore: 1.0},
				{Term: "humbler", TermNorm: "humbler", Kind: domain.VocabKindTrack, MatchScore: 0.5},
			}, nil
		},
	}
	p := &queryFakeProvider{name: domain.ProviderDeezer}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithVocabularyStore(store))

	out := runSearch(t, svc, "humble")
	svc.WaitForBackground()

	if out.CorrectedQuery != "" {
		t.Errorf("an exact vocabulary term must not be corrected, got %q", out.CorrectedQuery)
	}
	if len(out.Results) != 0 {
		t.Fatalf("want zero results, got %v", titles(out.Results))
	}
}
