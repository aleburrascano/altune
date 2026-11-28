package service_test

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/shared/clock"
	"context"
	"errors"
	"strings"
	"testing"
)

func humbleVocab() *memory.FakeVocabularyStore {
	return &memory.FakeVocabularyStore{
		FindClosestFn: func(_ string, _ int) ([]domain.SpellingHintsEntry, error) {
			return []domain.SpellingHintsEntry{
				{Term: "humble", TermNorm: "humble", Kind: domain.VocabKindTrack, MatchScore: 0.8},
			}, nil
		},
	}
}

func TestService_Execute_ZeroResultsTriggersCorrection(t *testing.T) {
	bg := memory.NewTracker(t)
	p := &memory.QueryFakeProvider{
		Provider: domain.ProviderDeezer,
		ResultsByQuery: map[string][]domain.SearchResult{
			"humble": {memory.DeezerTrack("HUMBLE.", "Kendrick Lamar", 80)},
		},
	}
	svc := memory.NewSearchServiceWith(t, bg, []ports.SearchProvider{p}, service.NewCircuitBreaker(clock.Now), func(c *service.Collaborators) { c.Vocabulary = humbleVocab() })

	out := runSearch(t, svc, "humbel")
	bg.Wait()

	if out.CorrectedQuery != "humble" {
		t.Errorf("want CorrectedQuery=%q, got %q", "humble", out.CorrectedQuery)
	}
	if out.OriginalQuery != "humbel" {
		t.Errorf("want OriginalQuery=%q, got %q", "humbel", out.OriginalQuery)
	}
	if len(out.Results) != 1 || out.Results[0].Title != "HUMBLE." {
		t.Fatalf("want the corrected search's result, got %v", memory.Titles(out.Results))
	}
}

func TestService_Execute_CorrectedResultsNeverCached(t *testing.T) {
	bg := memory.NewTracker(t)
	p := &memory.QueryFakeProvider{
		Provider: domain.ProviderDeezer,
		ResultsByQuery: map[string][]domain.SearchResult{
			"humble": {memory.DeezerTrack("HUMBLE.", "Kendrick Lamar", 80)},
		},
	}
	cache := memory.NewFakeResultCache()
	svc := memory.NewSearchServiceWith(t, bg, []ports.SearchProvider{p}, service.NewCircuitBreaker(clock.Now), func(c *service.Collaborators) { c.Vocabulary = humbleVocab(); c.ResultCache = cache })

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

func statusFor(t *testing.T, out *service.SearchOutput, provider domain.ProviderName) domain.ProviderSearchResponse {
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
	bg := memory.NewTracker(t)
	failsOriginal := &erroringThenFakeProvider{
		name:    domain.ProviderDeezer,
		failFor: "humbel",
		results: map[string][]domain.SearchResult{
			"humble": {memory.DeezerTrack("HUMBLE.", "Kendrick Lamar", 80)},
		},
	}
	healthy := &memory.QueryFakeProvider{
		Provider: domain.ProviderITunes,
		ResultsByQuery: map[string][]domain.SearchResult{
			"humble": {memory.Track("HUMBLE.", "Kendrick Lamar", domain.ProviderITunes, nil)},
		},
	}
	svc := memory.NewSearchServiceWith(t, bg, []ports.SearchProvider{failsOriginal, healthy}, service.NewCircuitBreaker(clock.Now), func(c *service.Collaborators) { c.Vocabulary = humbleVocab() })

	out := runSearch(t, svc, "humbel")
	bg.Wait()

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
	bg := memory.NewTracker(t)
	outage := errors.New("provider down")
	deezer := &memory.CountingProvider{Provider: domain.ProviderDeezer, Err: outage}
	itunes := &memory.CountingProvider{Provider: domain.ProviderITunes, Err: outage}
	vocab := humbleVocab()
	svc := memory.NewSearchServiceWith(t, bg, []ports.SearchProvider{deezer, itunes}, service.NewCircuitBreaker(clock.Now), func(c *service.Collaborators) { c.Vocabulary = vocab })

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
	bg := memory.NewTracker(t)
	failing := &memory.FakeProvider{Provider: domain.ProviderDeezer, Err: errors.New("boom")}
	empty := &memory.QueryFakeProvider{Provider: domain.ProviderITunes}
	svc := memory.NewSearchServiceWith(t, bg, []ports.SearchProvider{failing, empty}, service.NewCircuitBreaker(clock.Now), func(c *service.Collaborators) { c.Vocabulary = &memory.FakeVocabularyStore{} })

	out := runSearch(t, svc, "humbel")
	bg.Wait()

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
	bg := memory.NewTracker(t)
	store := humbleVocab()
	p := &memory.QueryFakeProvider{
		Provider: domain.ProviderDeezer,
		ResultsByQuery: map[string][]domain.SearchResult{
			"humble": {memory.DeezerTrack("HUMBLE.", "Kendrick Lamar", 80)},
		},
	}
	svc := memory.NewSearchServiceWith(t, bg, []ports.SearchProvider{p}, service.NewCircuitBreaker(clock.Now), func(c *service.Collaborators) { c.Vocabulary = store })

	out := runSearch(t, svc, "humble")
	bg.Wait()

	if out.CorrectedQuery != "" || out.OriginalQuery != "" {
		t.Errorf("want no correction, got corrected=%q original=%q", out.CorrectedQuery, out.OriginalQuery)
	}
	if len(out.Results) != 1 {
		t.Fatalf("want the direct result, got %v", memory.Titles(out.Results))
	}
	if store.FindClosestCalls != 0 {
		t.Errorf("correction must not run when the search has results, got %d FindClosest calls", store.FindClosestCalls)
	}
}

func TestService_Execute_NoCorrectionCandidateReturnsEmpty(t *testing.T) {
	bg := memory.NewTracker(t)
	store := &memory.FakeVocabularyStore{}
	p := &memory.QueryFakeProvider{Provider: domain.ProviderDeezer}
	svc := memory.NewSearchServiceWith(t, bg, []ports.SearchProvider{p}, service.NewCircuitBreaker(clock.Now), func(c *service.Collaborators) { c.Vocabulary = store })

	out := runSearch(t, svc, "humbel")
	bg.Wait()

	if len(out.Results) != 0 {
		t.Fatalf("want zero results, got %v", memory.Titles(out.Results))
	}
	if out.CorrectedQuery != "" || out.OriginalQuery != "" {
		t.Errorf("want no correction fields, got corrected=%q original=%q", out.CorrectedQuery, out.OriginalQuery)
	}
}

func TestService_Execute_CorrectedSearchAlsoEmptyReturnsEmpty(t *testing.T) {
	bg := memory.NewTracker(t)
	p := &memory.QueryFakeProvider{Provider: domain.ProviderDeezer}
	svc := memory.NewSearchServiceWith(t, bg, []ports.SearchProvider{p}, service.NewCircuitBreaker(clock.Now), func(c *service.Collaborators) { c.Vocabulary = humbleVocab() })

	out := runSearch(t, svc, "humbel")
	bg.Wait()

	if len(out.Results) != 0 {
		t.Fatalf("want zero results, got %v", memory.Titles(out.Results))
	}
	if out.CorrectedQuery != "" || out.OriginalQuery != "" {
		t.Errorf("a fruitless correction must not be surfaced, got corrected=%q original=%q", out.CorrectedQuery, out.OriginalQuery)
	}
}

func TestService_Execute_ExactVocabMatchNotCorrected(t *testing.T) {
	bg := memory.NewTracker(t)
	store := &memory.FakeVocabularyStore{
		FindClosestFn: func(_ string, _ int) ([]domain.SpellingHintsEntry, error) {
			return []domain.SpellingHintsEntry{
				{Term: "humble", TermNorm: "humble", Kind: domain.VocabKindTrack, MatchScore: 1.0},
				{Term: "humbler", TermNorm: "humbler", Kind: domain.VocabKindTrack, MatchScore: 0.5},
			}, nil
		},
	}
	p := &memory.QueryFakeProvider{Provider: domain.ProviderDeezer}
	svc := memory.NewSearchServiceWith(t, bg, []ports.SearchProvider{p}, service.NewCircuitBreaker(clock.Now), func(c *service.Collaborators) { c.Vocabulary = store })

	out := runSearch(t, svc, "humble")
	bg.Wait()

	if out.CorrectedQuery != "" {
		t.Errorf("an exact vocabulary term must not be corrected, got %q", out.CorrectedQuery)
	}
	if len(out.Results) != 0 {
		t.Fatalf("want zero results, got %v", memory.Titles(out.Results))
	}
}

func TestService_Correcting_DoesNotLogRawQueryText(t *testing.T) {
	bg := memory.NewTracker(t)
	buf := memory.CaptureLogs(t)

	const raw = "humbel"
	p := &memory.QueryFakeProvider{
		Provider: domain.ProviderDeezer,
		ResultsByQuery: map[string][]domain.SearchResult{
			"humble": {memory.DeezerTrack("HUMBLE.", "Kendrick Lamar", 80)},
		},
	}
	svc := memory.NewSearchServiceWith(t, bg, []ports.SearchProvider{p}, service.NewCircuitBreaker(clock.Now), func(c *service.Collaborators) { c.Vocabulary = humbleVocab() })
	runSearch(t, svc, raw)
	bg.Wait()

	event := memory.LoggedEvent(t, buf, "search.v2.correcting")
	if strings.Contains(event, raw) {
		t.Fatalf("correcting log leaks raw query text %q:\n%s", raw, event)
	}
	if !strings.Contains(event, `"fp"`) {
		t.Fatalf("expected a search-text fingerprint in place of raw text:\n%s", event)
	}
}

func TestCorrectTokens_DoesNotLogRawTokenText(t *testing.T) {
	buf := memory.CaptureLogs(t)

	store := &memory.FakeVocabularyStore{
		FindClosestFn: func(token string, _ int) ([]domain.SpellingHintsEntry, error) {
			if token == "humbel" {
				return []domain.SpellingHintsEntry{
					{Term: "humble", TermNorm: "humble", Kind: domain.VocabKindTrack, MatchScore: 0.8},
				}, nil
			}
			return nil, nil
		},
	}
	svc := memory.NewCorrection(t, store)

	result := svc.CorrectAggressive(context.Background(), "kendrick humbel")
	if result == nil || result.Corrected != "kendrick humble" {
		t.Fatalf("want token correction, got %+v", result)
	}

	event := memory.LoggedEvent(t, buf, "correction.token")
	if strings.Contains(event, "humbel") {
		t.Fatalf("token correction log leaks raw token text:\n%s", event)
	}
	if !strings.Contains(event, `"fp"`) {
		t.Fatalf("expected a search-text fingerprint in place of the raw token:\n%s", event)
	}
}

type deadlineRecordingVocab struct {
	memory.FakeVocabularyStore
	rec *memory.CtxRecordingVocab
}

func (v *deadlineRecordingVocab) SuggestByPrefix(ctx context.Context, prefix string, limit int) ([]domain.SpellingHintsEntry, error) {
	return v.rec.SuggestByPrefix(ctx, prefix, limit)
}

func (v *deadlineRecordingVocab) FindClosest(ctx context.Context, query string, limit int) ([]domain.SpellingHintsEntry, error) {
	return v.rec.FindClosest(ctx, query, limit)
}

func TestService_Execute_CorrectionRunsUnderADeadline(t *testing.T) {
	bg := memory.NewTracker(t)
	vocab := &memory.CtxRecordingVocab{}
	empty := &memory.FakeProvider{Provider: domain.ProviderDeezer}
	svc := memory.NewSearchServiceWith(t, bg, []ports.SearchProvider{empty}, service.NewCircuitBreaker(clock.Now), func(c *service.Collaborators) { c.Vocabulary = &deadlineRecordingVocab{rec: vocab} })
	query, err := domain.NewSearchQuery("kendrik lamar", map[domain.ResultKind]bool{domain.ResultKindArtist: true}, 10)
	if err != nil {
		t.Fatalf("NewSearchQuery: %v", err)
	}

	if _, err := svc.Execute(context.Background(), memory.NewUser(), query); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if vocab.FindClosestCalls == 0 {
		t.Fatal("correction never consulted the vocabulary")
	}
	if vocab.SawNoDeadline {
		t.Error("vocabulary lookups ran on a context with no deadline")
	}
}
