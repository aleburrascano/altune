package query_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/searchtest"
	"context"
	"errors"
	"strings"
	"testing"
)

func humbleVocab() *search.FakeVocabularyStore {
	return &search.FakeVocabularyStore{
		FindClosestFn: func(_ string, _ int) ([]search.SpellingHintsEntry, error) {
			return []search.SpellingHintsEntry{
				{Term: "humble", TermNorm: "humble", Kind: search.VocabKindTrack, MatchScore: 0.8},
			}, nil
		},
	}
}

func TestService_Execute_ZeroResultsTriggersCorrection(t *testing.T) {
	bg := searchtest.NewTracker(t)
	p := &search.QueryFakeProvider{
		Provider: search.ProviderDeezer,
		ResultsByQuery: map[string][]search.SearchResult{
			"humble": {search.DeezerTrack("HUMBLE.", "Kendrick Lamar", 80)},
		},
	}
	svc := search.NewSearchService(bg, []search.SearchProvider{p}, search.NewCircuitBreaker(), search.WithVocabularyStore(humbleVocab()))

	out := runSearch(t, svc, "humbel")
	bg.Wait()

	if out.CorrectedQuery != "humble" {
		t.Errorf("want CorrectedQuery=%q, got %q", "humble", out.CorrectedQuery)
	}
	if out.OriginalQuery != "humbel" {
		t.Errorf("want OriginalQuery=%q, got %q", "humbel", out.OriginalQuery)
	}
	if len(out.Results) != 1 || out.Results[0].Title != "HUMBLE." {
		t.Fatalf("want the corrected search's result, got %v", search.Titles(out.Results))
	}
}

func TestService_Execute_CorrectedResultsNeverCached(t *testing.T) {
	bg := searchtest.NewTracker(t)
	p := &search.QueryFakeProvider{
		Provider: search.ProviderDeezer,
		ResultsByQuery: map[string][]search.SearchResult{
			"humble": {search.DeezerTrack("HUMBLE.", "Kendrick Lamar", 80)},
		},
	}
	cache := search.NewFakeResultCache()
	svc := search.NewSearchService(bg, []search.SearchProvider{p}, search.NewCircuitBreaker(),
		search.WithVocabularyStore(humbleVocab()), search.WithResultCache(cache))

	out := runSearch(t, svc, "humbel")
	bg.Wait()

	if out.CorrectedQuery != "humble" {
		t.Fatalf("precondition: correction must fire, got %q", out.CorrectedQuery)
	}
	if cache.Sets != 0 {
		t.Errorf("corrected results must never be cached, sets = %d (store: %v)", cache.Sets, cache.Store)
	}
}

type erroringThenFakeProvider struct {
	name    search.ProviderName
	failFor string
	results map[string][]search.SearchResult
}

func (p *erroringThenFakeProvider) Name() search.ProviderName { return p.name }

func (p *erroringThenFakeProvider) Search(_ context.Context, query string, _ map[search.ResultKind]bool) ([]search.SearchResult, error) {
	if query == p.failFor {
		return nil, errors.New("boom")
	}
	return p.results[query], nil
}

func (p *erroringThenFakeProvider) SupportedKinds() map[search.ResultKind]bool {
	return map[search.ResultKind]bool{
		search.ResultKindTrack:  true,
		search.ResultKindAlbum:  true,
		search.ResultKindArtist: true,
	}
}

func statusFor(t *testing.T, out *search.SearchOutput, provider search.ProviderName) search.ProviderSearchResponse {
	t.Helper()
	for _, st := range out.ProviderStatuses {
		if st.Provider == provider {
			return st
		}
	}
	t.Fatalf("no status reported for %v, got %+v", provider, out.ProviderStatuses)
	return search.ProviderSearchResponse{}
}

func TestService_Execute_CorrectionKeepsFirstPassProviderFailure(t *testing.T) {
	bg := searchtest.NewTracker(t)
	failsOriginal := &erroringThenFakeProvider{
		name:    search.ProviderDeezer,
		failFor: "humbel",
		results: map[string][]search.SearchResult{
			"humble": {search.DeezerTrack("HUMBLE.", "Kendrick Lamar", 80)},
		},
	}
	healthy := &search.QueryFakeProvider{
		Provider: search.ProviderITunes,
		ResultsByQuery: map[string][]search.SearchResult{
			"humble": {search.Track("HUMBLE.", "Kendrick Lamar", search.ProviderITunes, nil)},
		},
	}
	svc := search.NewSearchService(bg, []search.SearchProvider{failsOriginal, healthy}, search.NewCircuitBreaker(),
		search.WithVocabularyStore(humbleVocab()))

	out := runSearch(t, svc, "humbel")
	bg.Wait()

	if out.CorrectedQuery != "humble" {
		t.Fatalf("precondition: correction must fire, got %q", out.CorrectedQuery)
	}
	if !out.Partial {
		t.Error("want partial=true: a provider was down for the query as asked")
	}
	if got := statusFor(t, out, search.ProviderDeezer); got.Status != search.ProviderStatusError {
		t.Errorf("failed provider reported as %+v, want the first pass's error", got)
	}
	healthyStatus := statusFor(t, out, search.ProviderITunes)
	if healthyStatus.Status != search.ProviderStatusOK || healthyStatus.ResultCount != 1 {
		t.Errorf("healthy provider = %+v, want the corrected pass's OK status with 1 result", healthyStatus)
	}
}

func TestService_Execute_TotalOutageSkipsCorrection(t *testing.T) {
	bg := searchtest.NewTracker(t)
	outage := errors.New("provider down")
	deezer := &search.CountingProvider{Provider: search.ProviderDeezer, Err: outage}
	itunes := &search.CountingProvider{Provider: search.ProviderITunes, Err: outage}
	vocab := humbleVocab()
	svc := search.NewSearchService(bg, []search.SearchProvider{deezer, itunes}, search.NewCircuitBreaker(),
		search.WithVocabularyStore(vocab))

	out := runSearch(t, svc, "humbel")
	bg.Wait()

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
	bg := searchtest.NewTracker(t)
	failing := &search.FakeProvider{Provider: search.ProviderDeezer, Err: errors.New("boom")}
	empty := &search.QueryFakeProvider{Provider: search.ProviderITunes}
	svc := search.NewSearchService(bg, []search.SearchProvider{failing, empty}, search.NewCircuitBreaker(),
		search.WithVocabularyStore(&search.FakeVocabularyStore{}))

	out := runSearch(t, svc, "humbel")
	bg.Wait()

	if out.CorrectedQuery != "" {
		t.Fatalf("precondition: no vocabulary candidate, so no correction; got %q", out.CorrectedQuery)
	}
	if !out.Partial {
		t.Error("want partial=true: a zero-result search must still report the failed provider")
	}
	if got := statusFor(t, out, search.ProviderDeezer); got.Status != search.ProviderStatusError {
		t.Errorf("failed provider reported as %+v, want the fan-out's error", got)
	}
}

func TestService_Execute_ResultsDoNotTriggerCorrection(t *testing.T) {
	bg := searchtest.NewTracker(t)
	store := humbleVocab()
	p := &search.QueryFakeProvider{
		Provider: search.ProviderDeezer,
		ResultsByQuery: map[string][]search.SearchResult{
			"humble": {search.DeezerTrack("HUMBLE.", "Kendrick Lamar", 80)},
		},
	}
	svc := search.NewSearchService(bg, []search.SearchProvider{p}, search.NewCircuitBreaker(), search.WithVocabularyStore(store))

	out := runSearch(t, svc, "humble")
	bg.Wait()

	if out.CorrectedQuery != "" || out.OriginalQuery != "" {
		t.Errorf("want no correction, got corrected=%q original=%q", out.CorrectedQuery, out.OriginalQuery)
	}
	if len(out.Results) != 1 {
		t.Fatalf("want the direct result, got %v", search.Titles(out.Results))
	}
	if store.FindClosestCalls != 0 {
		t.Errorf("correction must not run when the search has results, got %d FindClosest calls", store.FindClosestCalls)
	}
}

func TestService_Execute_NoCorrectionCandidateReturnsEmpty(t *testing.T) {
	bg := searchtest.NewTracker(t)
	store := &search.FakeVocabularyStore{}
	p := &search.QueryFakeProvider{Provider: search.ProviderDeezer}
	svc := search.NewSearchService(bg, []search.SearchProvider{p}, search.NewCircuitBreaker(), search.WithVocabularyStore(store))

	out := runSearch(t, svc, "humbel")
	bg.Wait()

	if len(out.Results) != 0 {
		t.Fatalf("want zero results, got %v", search.Titles(out.Results))
	}
	if out.CorrectedQuery != "" || out.OriginalQuery != "" {
		t.Errorf("want no correction fields, got corrected=%q original=%q", out.CorrectedQuery, out.OriginalQuery)
	}
}

func TestService_Execute_CorrectedSearchAlsoEmptyReturnsEmpty(t *testing.T) {
	bg := searchtest.NewTracker(t)
	p := &search.QueryFakeProvider{Provider: search.ProviderDeezer}
	svc := search.NewSearchService(bg, []search.SearchProvider{p}, search.NewCircuitBreaker(), search.WithVocabularyStore(humbleVocab()))

	out := runSearch(t, svc, "humbel")
	bg.Wait()

	if len(out.Results) != 0 {
		t.Fatalf("want zero results, got %v", search.Titles(out.Results))
	}
	if out.CorrectedQuery != "" || out.OriginalQuery != "" {
		t.Errorf("a fruitless correction must not be surfaced, got corrected=%q original=%q", out.CorrectedQuery, out.OriginalQuery)
	}
}

func TestService_Execute_ExactVocabMatchNotCorrected(t *testing.T) {
	bg := searchtest.NewTracker(t)
	store := &search.FakeVocabularyStore{
		FindClosestFn: func(_ string, _ int) ([]search.SpellingHintsEntry, error) {
			return []search.SpellingHintsEntry{
				{Term: "humble", TermNorm: "humble", Kind: search.VocabKindTrack, MatchScore: 1.0},
				{Term: "humbler", TermNorm: "humbler", Kind: search.VocabKindTrack, MatchScore: 0.5},
			}, nil
		},
	}
	p := &search.QueryFakeProvider{Provider: search.ProviderDeezer}
	svc := search.NewSearchService(bg, []search.SearchProvider{p}, search.NewCircuitBreaker(), search.WithVocabularyStore(store))

	out := runSearch(t, svc, "humble")
	bg.Wait()

	if out.CorrectedQuery != "" {
		t.Errorf("an exact vocabulary term must not be corrected, got %q", out.CorrectedQuery)
	}
	if len(out.Results) != 0 {
		t.Fatalf("want zero results, got %v", search.Titles(out.Results))
	}
}

func TestService_Correcting_DoesNotLogRawQueryText(t *testing.T) {
	bg := searchtest.NewTracker(t)
	buf := search.CaptureLogs(t)

	const raw = "humbel"
	p := &search.QueryFakeProvider{
		Provider: search.ProviderDeezer,
		ResultsByQuery: map[string][]search.SearchResult{
			"humble": {search.DeezerTrack("HUMBLE.", "Kendrick Lamar", 80)},
		},
	}
	svc := search.NewSearchService(bg, []search.SearchProvider{p}, search.NewCircuitBreaker(), search.WithVocabularyStore(humbleVocab()))
	runSearch(t, svc, raw)
	bg.Wait()

	event := search.LoggedEvent(t, buf, "search.v2.correcting")
	if strings.Contains(event, raw) {
		t.Fatalf("correcting log leaks raw query text %q:\n%s", raw, event)
	}
	if !strings.Contains(event, `"fp"`) {
		t.Fatalf("expected a search-text fingerprint in place of raw text:\n%s", event)
	}
}

func TestCorrectTokens_DoesNotLogRawTokenText(t *testing.T) {
	buf := search.CaptureLogs(t)

	store := &search.FakeVocabularyStore{
		FindClosestFn: func(token string, _ int) ([]search.SpellingHintsEntry, error) {
			if token == "humbel" {
				return []search.SpellingHintsEntry{
					{Term: "humble", TermNorm: "humble", Kind: search.VocabKindTrack, MatchScore: 0.8},
				}, nil
			}
			return nil, nil
		},
	}
	svc := search.NewCorrectionService(store)

	result := svc.CorrectAggressive(context.Background(), "kendrick humbel")
	if result == nil || result.Corrected != "kendrick humble" {
		t.Fatalf("want token correction, got %+v", result)
	}

	event := search.LoggedEvent(t, buf, "correction.token")
	if strings.Contains(event, "humbel") {
		t.Fatalf("token correction log leaks raw token text:\n%s", event)
	}
	if !strings.Contains(event, `"fp"`) {
		t.Fatalf("expected a search-text fingerprint in place of the raw token:\n%s", event)
	}
}

type deadlineRecordingVocab struct {
	search.FakeVocabularyStore
	rec *search.CtxRecordingVocab
}

func (v *deadlineRecordingVocab) SuggestByPrefix(ctx context.Context, prefix string, limit int) ([]search.SpellingHintsEntry, error) {
	return v.rec.SuggestByPrefix(ctx, prefix, limit)
}

func (v *deadlineRecordingVocab) FindClosest(ctx context.Context, query string, limit int) ([]search.SpellingHintsEntry, error) {
	return v.rec.FindClosest(ctx, query, limit)
}

func TestService_Execute_CorrectionRunsUnderADeadline(t *testing.T) {
	bg := searchtest.NewTracker(t)
	vocab := &search.CtxRecordingVocab{}
	empty := &search.FakeProvider{Provider: search.ProviderDeezer}
	svc := search.NewSearchService(bg, []search.SearchProvider{empty}, search.NewCircuitBreaker(), search.WithVocabularyStore(&deadlineRecordingVocab{rec: vocab}))
	query, err := search.NewSearchQuery("kendrik lamar", map[search.ResultKind]bool{search.ResultKindArtist: true}, 10)
	if err != nil {
		t.Fatalf("NewSearchQuery: %v", err)
	}

	if _, err := svc.Execute(context.Background(), search.NewUser(), query); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if vocab.FindClosestCalls == 0 {
		t.Fatal("correction never consulted the vocabulary")
	}
	if vocab.SawNoDeadline {
		t.Error("vocabulary lookups ran on a context with no deadline")
	}
}
