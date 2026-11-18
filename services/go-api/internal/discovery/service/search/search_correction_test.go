package search_test

import (
	"altune/go-api/internal/discovery"
	"context"
	"errors"
	"strings"
	"testing"
)

func humbleVocab() *discovery.FakeVocabularyStore {
	return &discovery.FakeVocabularyStore{
		FindClosestFn: func(_ string, _ int) ([]discovery.VocabularyEntry, error) {
			return []discovery.VocabularyEntry{
				{Term: "humble", TermNorm: "humble", Kind: discovery.VocabKindTrack, MatchScore: 0.8},
			}, nil
		},
	}
}

func TestService_Execute_ZeroResultsTriggersCorrection(t *testing.T) {
	p := &discovery.QueryFakeProvider{
		Provider: discovery.ProviderDeezer,
		ResultsByQuery: map[string][]discovery.SearchResult{
			"humble": {discovery.DeezerTrack("HUMBLE.", "Kendrick Lamar", 80)},
		},
	}
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker(), discovery.WithVocabularyStore(humbleVocab()))

	out := runSearch(t, svc, "humbel")
	svc.WaitForBackground()

	if out.CorrectedQuery != "humble" {
		t.Errorf("want CorrectedQuery=%q, got %q", "humble", out.CorrectedQuery)
	}
	if out.OriginalQuery != "humbel" {
		t.Errorf("want OriginalQuery=%q, got %q", "humbel", out.OriginalQuery)
	}
	if len(out.Results) != 1 || out.Results[0].Title != "HUMBLE." {
		t.Fatalf("want the corrected search's result, got %v", discovery.Titles(out.Results))
	}
}

func TestService_Execute_CorrectedResultsNeverCached(t *testing.T) {
	p := &discovery.QueryFakeProvider{
		Provider: discovery.ProviderDeezer,
		ResultsByQuery: map[string][]discovery.SearchResult{
			"humble": {discovery.DeezerTrack("HUMBLE.", "Kendrick Lamar", 80)},
		},
	}
	cache := discovery.NewFakeResultCache()
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker(),
		discovery.WithVocabularyStore(humbleVocab()), discovery.WithResultCache(cache))

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
	name    discovery.ProviderName
	failFor string
	results map[string][]discovery.SearchResult
}

func (p *erroringThenFakeProvider) Name() discovery.ProviderName { return p.name }

func (p *erroringThenFakeProvider) Search(_ context.Context, query string, _ map[discovery.ResultKind]bool) ([]discovery.SearchResult, error) {
	if query == p.failFor {
		return nil, errors.New("boom")
	}
	return p.results[query], nil
}

func (p *erroringThenFakeProvider) SupportedKinds() map[discovery.ResultKind]bool {
	return map[discovery.ResultKind]bool{
		discovery.ResultKindTrack:  true,
		discovery.ResultKindAlbum:  true,
		discovery.ResultKindArtist: true,
	}
}

func statusFor(t *testing.T, out *discovery.SearchOutput, provider discovery.ProviderName) discovery.ProviderSearchResponse {
	t.Helper()
	for _, st := range out.ProviderStatuses {
		if st.Provider == provider {
			return st
		}
	}
	t.Fatalf("no status reported for %v, got %+v", provider, out.ProviderStatuses)
	return discovery.ProviderSearchResponse{}
}

func TestService_Execute_CorrectionKeepsFirstPassProviderFailure(t *testing.T) {
	failsOriginal := &erroringThenFakeProvider{
		name:    discovery.ProviderDeezer,
		failFor: "humbel",
		results: map[string][]discovery.SearchResult{
			"humble": {discovery.DeezerTrack("HUMBLE.", "Kendrick Lamar", 80)},
		},
	}
	healthy := &discovery.QueryFakeProvider{
		Provider: discovery.ProviderITunes,
		ResultsByQuery: map[string][]discovery.SearchResult{
			"humble": {discovery.Track("HUMBLE.", "Kendrick Lamar", discovery.ProviderITunes, nil)},
		},
	}
	svc := discovery.NewSearchService([]discovery.SearchProvider{failsOriginal, healthy}, discovery.NewCircuitBreaker(),
		discovery.WithVocabularyStore(humbleVocab()))

	out := runSearch(t, svc, "humbel")
	svc.WaitForBackground()

	if out.CorrectedQuery != "humble" {
		t.Fatalf("precondition: correction must fire, got %q", out.CorrectedQuery)
	}
	if !out.Partial {
		t.Error("want partial=true: a provider was down for the query as asked")
	}
	if got := statusFor(t, out, discovery.ProviderDeezer); got.Status != discovery.ProviderStatusError {
		t.Errorf("failed provider reported as %+v, want the first pass's error", got)
	}
	healthyStatus := statusFor(t, out, discovery.ProviderITunes)
	if healthyStatus.Status != discovery.ProviderStatusOK || healthyStatus.ResultCount != 1 {
		t.Errorf("healthy provider = %+v, want the corrected pass's OK status with 1 result", healthyStatus)
	}
}

func TestService_Execute_TotalOutageSkipsCorrection(t *testing.T) {
	outage := errors.New("provider down")
	deezer := &discovery.CountingProvider{Provider: discovery.ProviderDeezer, Err: outage}
	itunes := &discovery.CountingProvider{Provider: discovery.ProviderITunes, Err: outage}
	vocab := humbleVocab()
	svc := discovery.NewSearchService([]discovery.SearchProvider{deezer, itunes}, discovery.NewCircuitBreaker(),
		discovery.WithVocabularyStore(vocab))

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
	failing := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Err: errors.New("boom")}
	empty := &discovery.QueryFakeProvider{Provider: discovery.ProviderITunes}
	svc := discovery.NewSearchService([]discovery.SearchProvider{failing, empty}, discovery.NewCircuitBreaker(),
		discovery.WithVocabularyStore(&discovery.FakeVocabularyStore{}))

	out := runSearch(t, svc, "humbel")
	svc.WaitForBackground()

	if out.CorrectedQuery != "" {
		t.Fatalf("precondition: no vocabulary candidate, so no correction; got %q", out.CorrectedQuery)
	}
	if !out.Partial {
		t.Error("want partial=true: a zero-result search must still report the failed provider")
	}
	if got := statusFor(t, out, discovery.ProviderDeezer); got.Status != discovery.ProviderStatusError {
		t.Errorf("failed provider reported as %+v, want the fan-out's error", got)
	}
}

func TestService_Execute_ResultsDoNotTriggerCorrection(t *testing.T) {
	store := humbleVocab()
	p := &discovery.QueryFakeProvider{
		Provider: discovery.ProviderDeezer,
		ResultsByQuery: map[string][]discovery.SearchResult{
			"humble": {discovery.DeezerTrack("HUMBLE.", "Kendrick Lamar", 80)},
		},
	}
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker(), discovery.WithVocabularyStore(store))

	out := runSearch(t, svc, "humble")
	svc.WaitForBackground()

	if out.CorrectedQuery != "" || out.OriginalQuery != "" {
		t.Errorf("want no correction, got corrected=%q original=%q", out.CorrectedQuery, out.OriginalQuery)
	}
	if len(out.Results) != 1 {
		t.Fatalf("want the direct result, got %v", discovery.Titles(out.Results))
	}
	if store.FindClosestCalls != 0 {
		t.Errorf("correction must not run when the search has results, got %d FindClosest calls", store.FindClosestCalls)
	}
}

func TestService_Execute_NoCorrectionCandidateReturnsEmpty(t *testing.T) {
	store := &discovery.FakeVocabularyStore{}
	p := &discovery.QueryFakeProvider{Provider: discovery.ProviderDeezer}
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker(), discovery.WithVocabularyStore(store))

	out := runSearch(t, svc, "humbel")
	svc.WaitForBackground()

	if len(out.Results) != 0 {
		t.Fatalf("want zero results, got %v", discovery.Titles(out.Results))
	}
	if out.CorrectedQuery != "" || out.OriginalQuery != "" {
		t.Errorf("want no correction fields, got corrected=%q original=%q", out.CorrectedQuery, out.OriginalQuery)
	}
}

func TestService_Execute_CorrectedSearchAlsoEmptyReturnsEmpty(t *testing.T) {
	p := &discovery.QueryFakeProvider{Provider: discovery.ProviderDeezer}
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker(), discovery.WithVocabularyStore(humbleVocab()))

	out := runSearch(t, svc, "humbel")
	svc.WaitForBackground()

	if len(out.Results) != 0 {
		t.Fatalf("want zero results, got %v", discovery.Titles(out.Results))
	}
	if out.CorrectedQuery != "" || out.OriginalQuery != "" {
		t.Errorf("a fruitless correction must not be surfaced, got corrected=%q original=%q", out.CorrectedQuery, out.OriginalQuery)
	}
}

func TestService_Execute_ExactVocabMatchNotCorrected(t *testing.T) {
	store := &discovery.FakeVocabularyStore{
		FindClosestFn: func(_ string, _ int) ([]discovery.VocabularyEntry, error) {
			return []discovery.VocabularyEntry{
				{Term: "humble", TermNorm: "humble", Kind: discovery.VocabKindTrack, MatchScore: 1.0},
				{Term: "humbler", TermNorm: "humbler", Kind: discovery.VocabKindTrack, MatchScore: 0.5},
			}, nil
		},
	}
	p := &discovery.QueryFakeProvider{Provider: discovery.ProviderDeezer}
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker(), discovery.WithVocabularyStore(store))

	out := runSearch(t, svc, "humble")
	svc.WaitForBackground()

	if out.CorrectedQuery != "" {
		t.Errorf("an exact vocabulary term must not be corrected, got %q", out.CorrectedQuery)
	}
	if len(out.Results) != 0 {
		t.Fatalf("want zero results, got %v", discovery.Titles(out.Results))
	}
}

func TestService_Correcting_DoesNotLogRawQueryText(t *testing.T) {
	buf := discovery.CaptureLogs(t)

	const raw = "humbel"
	p := &discovery.QueryFakeProvider{
		Provider: discovery.ProviderDeezer,
		ResultsByQuery: map[string][]discovery.SearchResult{
			"humble": {discovery.DeezerTrack("HUMBLE.", "Kendrick Lamar", 80)},
		},
	}
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker(), discovery.WithVocabularyStore(humbleVocab()))
	runSearch(t, svc, raw)
	svc.WaitForBackground()

	event := discovery.LoggedEvent(t, buf, "search.v2.correcting")
	if strings.Contains(event, raw) {
		t.Fatalf("correcting log leaks raw query text %q:\n%s", raw, event)
	}
	if !strings.Contains(event, `"fp"`) {
		t.Fatalf("expected a search-text fingerprint in place of raw text:\n%s", event)
	}
}

func TestCorrectTokens_DoesNotLogRawTokenText(t *testing.T) {
	buf := discovery.CaptureLogs(t)

	store := &discovery.FakeVocabularyStore{
		FindClosestFn: func(token string, _ int) ([]discovery.VocabularyEntry, error) {
			if token == "humbel" {
				return []discovery.VocabularyEntry{
					{Term: "humble", TermNorm: "humble", Kind: discovery.VocabKindTrack, MatchScore: 0.8},
				}, nil
			}
			return nil, nil
		},
	}
	svc := discovery.NewCorrectionService(store)

	result := svc.CorrectAggressive(context.Background(), "kendrick humbel")
	if result == nil || result.Corrected != "kendrick humble" {
		t.Fatalf("want token correction, got %+v", result)
	}

	event := discovery.LoggedEvent(t, buf, "correction.token")
	if strings.Contains(event, "humbel") {
		t.Fatalf("token correction log leaks raw token text:\n%s", event)
	}
	if !strings.Contains(event, `"fp"`) {
		t.Fatalf("expected a search-text fingerprint in place of the raw token:\n%s", event)
	}
}

type deadlineRecordingVocab struct {
	discovery.FakeVocabularyStore
	rec *discovery.CtxRecordingVocab
}

func (v *deadlineRecordingVocab) SuggestByPrefix(ctx context.Context, prefix string, limit int) ([]discovery.VocabularyEntry, error) {
	return v.rec.SuggestByPrefix(ctx, prefix, limit)
}

func (v *deadlineRecordingVocab) FindClosest(ctx context.Context, query string, limit int) ([]discovery.VocabularyEntry, error) {
	return v.rec.FindClosest(ctx, query, limit)
}

func TestService_Execute_CorrectionRunsUnderADeadline(t *testing.T) {
	vocab := &discovery.CtxRecordingVocab{}
	empty := &discovery.FakeProvider{Provider: discovery.ProviderDeezer}
	svc := discovery.NewSearchService([]discovery.SearchProvider{empty}, discovery.NewCircuitBreaker(), discovery.WithVocabularyStore(&deadlineRecordingVocab{rec: vocab}))
	query, err := discovery.NewSearchQuery("kendrik lamar", map[discovery.ResultKind]bool{discovery.ResultKindArtist: true}, 10)
	if err != nil {
		t.Fatalf("NewSearchQuery: %v", err)
	}

	if _, err := svc.Execute(context.Background(), discovery.NewUser(), query, false); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if vocab.FindClosestCalls == 0 {
		t.Fatal("correction never consulted the vocabulary")
	}
	if vocab.SawNoDeadline {
		t.Error("vocabulary lookups ran on a context with no deadline")
	}
}
