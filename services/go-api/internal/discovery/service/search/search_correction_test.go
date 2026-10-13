package search

import (
	"altune/go-api/internal/discovery/discoverytest"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service/providercall"
	"altune/go-api/internal/discovery/service/vocabulary"
	"context"
	"errors"
	"strings"
	"testing"
)

func humbleVocab() *discoverytest.FakeVocabularyStore {
	return &discoverytest.FakeVocabularyStore{
		FindClosestFn: func(_ string, _ int) ([]domain.VocabularyEntry, error) {
			return []domain.VocabularyEntry{
				{Term: "humble", TermNorm: "humble", Kind: domain.VocabKindTrack, MatchScore: 0.8},
			}, nil
		},
	}
}

func TestService_Execute_ZeroResultsTriggersCorrection(t *testing.T) {
	p := &discoverytest.QueryFakeProvider{
		Provider: domain.ProviderDeezer,
		ResultsByQuery: map[string][]domain.SearchResult{
			"humble": {discoverytest.DeezerTrack("HUMBLE.", "Kendrick Lamar", 80)},
		},
	}
	svc := NewService([]ports.SearchProvider{p}, providercall.NewCircuitBreaker(), WithVocabularyStore(humbleVocab()))

	out := runSearch(t, svc, "humbel")
	svc.WaitForBackground()

	if out.CorrectedQuery != "humble" {
		t.Errorf("want CorrectedQuery=%q, got %q", "humble", out.CorrectedQuery)
	}
	if out.OriginalQuery != "humbel" {
		t.Errorf("want OriginalQuery=%q, got %q", "humbel", out.OriginalQuery)
	}
	if len(out.Results) != 1 || out.Results[0].Title != "HUMBLE." {
		t.Fatalf("want the corrected search's result, got %v", discoverytest.Titles(out.Results))
	}
}

func TestService_Execute_CorrectedResultsNeverCached(t *testing.T) {
	p := &discoverytest.QueryFakeProvider{
		Provider: domain.ProviderDeezer,
		ResultsByQuery: map[string][]domain.SearchResult{
			"humble": {discoverytest.DeezerTrack("HUMBLE.", "Kendrick Lamar", 80)},
		},
	}
	cache := discoverytest.NewFakeResultCache()
	svc := NewService([]ports.SearchProvider{p}, providercall.NewCircuitBreaker(),
		WithVocabularyStore(humbleVocab()), WithResultCache(cache))

	out := runSearch(t, svc, "humbel")
	svc.WaitForBackground()

	if out.CorrectedQuery != "humble" {
		t.Fatalf("precondition: correction must fire, got %q", out.CorrectedQuery)
	}
	if cache.Sets != 0 {
		t.Errorf("corrected results must never be cached, sets = %d (store: %v)", cache.Sets, cache.Store)
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
			"humble": {discoverytest.DeezerTrack("HUMBLE.", "Kendrick Lamar", 80)},
		},
	}
	healthy := &discoverytest.QueryFakeProvider{
		Provider: domain.ProviderITunes,
		ResultsByQuery: map[string][]domain.SearchResult{
			"humble": {discoverytest.Track("HUMBLE.", "Kendrick Lamar", domain.ProviderITunes, nil)},
		},
	}
	svc := NewService([]ports.SearchProvider{failsOriginal, healthy}, providercall.NewCircuitBreaker(),
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
	deezer := &discoverytest.CountingProvider{Provider: domain.ProviderDeezer, Err: outage}
	itunes := &discoverytest.CountingProvider{Provider: domain.ProviderITunes, Err: outage}
	vocab := humbleVocab()
	svc := NewService([]ports.SearchProvider{deezer, itunes}, providercall.NewCircuitBreaker(),
		WithVocabularyStore(vocab))

	out := runSearch(t, svc, "humbel")
	svc.WaitForBackground()

	if deezer.Calls != 1 || itunes.Calls != 1 {
		t.Errorf("searches = deezer %d, itunes %d; want 1 each (no correction retry into an outage)",
			deezer.Calls, itunes.Calls)
	}
	if vocab.FindClosestCalls != 0 {
		t.Errorf("vocabulary consulted %d times during a total outage, want 0", vocab.FindClosestCalls)
	}
	if out.CorrectedQuery != "" {
		t.Errorf("an outage is not a spelling miss, got corrected=%q", out.CorrectedQuery)
	}
	if !out.Partial {
		t.Error("want partial=true: every provider failed")
	}
}

func TestService_Execute_NoCorrectionKeepsFanOutStatuses(t *testing.T) {
	failing := &discoverytest.FakeProvider{Provider: domain.ProviderDeezer, Err: errors.New("boom")}
	empty := &discoverytest.QueryFakeProvider{Provider: domain.ProviderITunes}
	svc := NewService([]ports.SearchProvider{failing, empty}, providercall.NewCircuitBreaker(),
		WithVocabularyStore(&discoverytest.FakeVocabularyStore{}))

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
	p := &discoverytest.QueryFakeProvider{
		Provider: domain.ProviderDeezer,
		ResultsByQuery: map[string][]domain.SearchResult{
			"humble": {discoverytest.DeezerTrack("HUMBLE.", "Kendrick Lamar", 80)},
		},
	}
	svc := NewService([]ports.SearchProvider{p}, providercall.NewCircuitBreaker(), WithVocabularyStore(store))

	out := runSearch(t, svc, "humble")
	svc.WaitForBackground()

	if out.CorrectedQuery != "" || out.OriginalQuery != "" {
		t.Errorf("want no correction, got corrected=%q original=%q", out.CorrectedQuery, out.OriginalQuery)
	}
	if len(out.Results) != 1 {
		t.Fatalf("want the direct result, got %v", discoverytest.Titles(out.Results))
	}
	if store.FindClosestCalls != 0 {
		t.Errorf("correction must not run when the search has results, got %d FindClosest calls", store.FindClosestCalls)
	}
}

func TestService_Execute_NoCorrectionCandidateReturnsEmpty(t *testing.T) {
	store := &discoverytest.FakeVocabularyStore{}
	p := &discoverytest.QueryFakeProvider{Provider: domain.ProviderDeezer}
	svc := NewService([]ports.SearchProvider{p}, providercall.NewCircuitBreaker(), WithVocabularyStore(store))

	out := runSearch(t, svc, "humbel")
	svc.WaitForBackground()

	if len(out.Results) != 0 {
		t.Fatalf("want zero results, got %v", discoverytest.Titles(out.Results))
	}
	if out.CorrectedQuery != "" || out.OriginalQuery != "" {
		t.Errorf("want no correction fields, got corrected=%q original=%q", out.CorrectedQuery, out.OriginalQuery)
	}
}

func TestService_Execute_CorrectedSearchAlsoEmptyReturnsEmpty(t *testing.T) {
	p := &discoverytest.QueryFakeProvider{Provider: domain.ProviderDeezer}
	svc := NewService([]ports.SearchProvider{p}, providercall.NewCircuitBreaker(), WithVocabularyStore(humbleVocab()))

	out := runSearch(t, svc, "humbel")
	svc.WaitForBackground()

	if len(out.Results) != 0 {
		t.Fatalf("want zero results, got %v", discoverytest.Titles(out.Results))
	}
	if out.CorrectedQuery != "" || out.OriginalQuery != "" {
		t.Errorf("a fruitless correction must not be surfaced, got corrected=%q original=%q", out.CorrectedQuery, out.OriginalQuery)
	}
}

func TestService_Execute_ExactVocabMatchNotCorrected(t *testing.T) {
	store := &discoverytest.FakeVocabularyStore{
		FindClosestFn: func(_ string, _ int) ([]domain.VocabularyEntry, error) {
			return []domain.VocabularyEntry{
				{Term: "humble", TermNorm: "humble", Kind: domain.VocabKindTrack, MatchScore: 1.0},
				{Term: "humbler", TermNorm: "humbler", Kind: domain.VocabKindTrack, MatchScore: 0.5},
			}, nil
		},
	}
	p := &discoverytest.QueryFakeProvider{Provider: domain.ProviderDeezer}
	svc := NewService([]ports.SearchProvider{p}, providercall.NewCircuitBreaker(), WithVocabularyStore(store))

	out := runSearch(t, svc, "humble")
	svc.WaitForBackground()

	if out.CorrectedQuery != "" {
		t.Errorf("an exact vocabulary term must not be corrected, got %q", out.CorrectedQuery)
	}
	if len(out.Results) != 0 {
		t.Fatalf("want zero results, got %v", discoverytest.Titles(out.Results))
	}
}

func TestService_Correcting_DoesNotLogRawQueryText(t *testing.T) {
	buf := discoverytest.CaptureLogs(t)

	const raw = "humbel"
	p := &discoverytest.QueryFakeProvider{
		Provider: domain.ProviderDeezer,
		ResultsByQuery: map[string][]domain.SearchResult{
			"humble": {discoverytest.DeezerTrack("HUMBLE.", "Kendrick Lamar", 80)},
		},
	}
	svc := NewService([]ports.SearchProvider{p}, providercall.NewCircuitBreaker(), WithVocabularyStore(humbleVocab()))
	runSearch(t, svc, raw)
	svc.WaitForBackground()

	event := discoverytest.LoggedEvent(t, buf, "search.v2.correcting")
	if strings.Contains(event, raw) {
		t.Fatalf("correcting log leaks raw query text %q:\n%s", raw, event)
	}
	if !strings.Contains(event, `"fp"`) {
		t.Fatalf("expected a search-text fingerprint in place of raw text:\n%s", event)
	}
}

func TestCorrectTokens_DoesNotLogRawTokenText(t *testing.T) {
	buf := discoverytest.CaptureLogs(t)

	store := &discoverytest.FakeVocabularyStore{
		FindClosestFn: func(token string, _ int) ([]domain.VocabularyEntry, error) {
			if token == "humbel" {
				return []domain.VocabularyEntry{
					{Term: "humble", TermNorm: "humble", Kind: domain.VocabKindTrack, MatchScore: 0.8},
				}, nil
			}
			return nil, nil
		},
	}
	svc := vocabulary.NewCorrectionService(store)

	result := svc.CorrectAggressive(context.Background(), "kendrick humbel")
	if result == nil || result.Corrected != "kendrick humble" {
		t.Fatalf("want token correction, got %+v", result)
	}

	event := discoverytest.LoggedEvent(t, buf, "correction.token")
	if strings.Contains(event, "humbel") {
		t.Fatalf("token correction log leaks raw token text:\n%s", event)
	}
	if !strings.Contains(event, `"fp"`) {
		t.Fatalf("expected a search-text fingerprint in place of the raw token:\n%s", event)
	}
}

func TestTryCorrection_RunsUnderExplicitDeadline(t *testing.T) {
	vocab := &discoverytest.CtxRecordingVocab{}
	s := &Service{correctionSvc: vocabulary.NewCorrectionService(vocab)}
	query, err := domain.NewSearchQuery("kendrik lamar", map[domain.ResultKind]bool{domain.ResultKindArtist: true}, 10)
	if err != nil {
		t.Fatalf("NewSearchQuery: %v", err)
	}

	s.tryCorrection(context.Background(), query)

	if vocab.FindClosestCalls == 0 {
		t.Fatal("correction never consulted the vocabulary")
	}
	if vocab.SawNoDeadline {
		t.Error("vocabulary lookups ran on a context with no deadline")
	}
}
