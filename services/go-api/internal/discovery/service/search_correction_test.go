package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"errors"
	"testing"
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

func statusFor(t *testing.T, out *SearchOutput, provider domain.ProviderName) domain.ProviderSearchResponse {
	t.Helper()
	for _, st := range out.ProviderStatuses {
		if st.Provider == provider {
			return st
		}
	}
	t.Fatalf("no status reported for %v, got %+v", provider, out.ProviderStatuses)
	return domain.ProviderSearchResponse{}
}

func TestService_Execute_CorrectionKeepsFirstPassProviderFailure(t *testing.T) {
	failsOriginal := &erroringThenFakeProvider{
		name:    domain.ProviderDeezer,
		failFor: "humbel",
		results: map[string][]domain.SearchResult{
			"humble": {deezerTrack("HUMBLE.", "Kendrick Lamar", 80)},
		},
	}
	healthy := &queryFakeProvider{
		name: domain.ProviderITunes,
		resultsByQuery: map[string][]domain.SearchResult{
			"humble": {track("HUMBLE.", "Kendrick Lamar", domain.ProviderITunes, nil)},
		},
	}
	svc := NewService([]ports.SearchProvider{failsOriginal, healthy}, NewCircuitBreaker(),
		WithVocabularyStore(humbleVocab()))

	out := runSearch(t, svc, "humbel")
	svc.WaitForBackground()

	if out.CorrectedQuery != "humble" {
		t.Fatalf("precondition: correction must fire, got %q", out.CorrectedQuery)
	}
	if !out.Partial {
		t.Error("want partial=true: a provider was down for the query as asked")
	}
	if got := statusFor(t, out, domain.ProviderDeezer); got.Status != domain.ProviderStatusError {
		t.Errorf("failed provider reported as %+v, want the first pass's error", got)
	}
	healthyStatus := statusFor(t, out, domain.ProviderITunes)
	if healthyStatus.Status != domain.ProviderStatusOK || healthyStatus.ResultCount != 1 {
		t.Errorf("healthy provider = %+v, want the corrected pass's OK status with 1 result", healthyStatus)
	}
}

func TestService_Execute_TotalOutageSkipsCorrection(t *testing.T) {
	outage := errors.New("provider down")
	deezer := &countingProvider{name: domain.ProviderDeezer, err: outage}
	itunes := &countingProvider{name: domain.ProviderITunes, err: outage}
	vocab := humbleVocab()
	svc := NewService([]ports.SearchProvider{deezer, itunes}, NewCircuitBreaker(),
		WithVocabularyStore(vocab))

	out := runSearch(t, svc, "humbel")
	svc.WaitForBackground()

	if deezer.calls != 1 || itunes.calls != 1 {
		t.Errorf("searches = deezer %d, itunes %d; want 1 each (no correction retry into an outage)",
			deezer.calls, itunes.calls)
	}
	if vocab.findClosestCalls != 0 {
		t.Errorf("vocabulary consulted %d times during a total outage, want 0", vocab.findClosestCalls)
	}
	if out.CorrectedQuery != "" {
		t.Errorf("an outage is not a spelling miss, got corrected=%q", out.CorrectedQuery)
	}
	if !out.Partial {
		t.Error("want partial=true: every provider failed")
	}
}

func TestService_Execute_NoCorrectionKeepsFanOutStatuses(t *testing.T) {
	failing := &fakeProvider{name: domain.ProviderDeezer, err: errors.New("boom")}
	empty := &queryFakeProvider{name: domain.ProviderITunes}
	svc := NewService([]ports.SearchProvider{failing, empty}, NewCircuitBreaker(),
		WithVocabularyStore(&fakeVocabularyStore{}))

	out := runSearch(t, svc, "humbel")
	svc.WaitForBackground()

	if out.CorrectedQuery != "" {
		t.Fatalf("precondition: no vocabulary candidate, so no correction; got %q", out.CorrectedQuery)
	}
	if !out.Partial {
		t.Error("want partial=true: a zero-result search must still report the failed provider")
	}
	if got := statusFor(t, out, domain.ProviderDeezer); got.Status != domain.ProviderStatusError {
		t.Errorf("failed provider reported as %+v, want the fan-out's error", got)
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
