package service_test

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/shared/clock"
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
)

func TestService_ResultCache_HitSkipsProvidersAndIsCrossUser(t *testing.T) {
	bg := memory.NewTracker(t)
	p := &memory.CountingProvider{Provider: domain.ProviderDeezer, Results: []domain.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	cache := memory.NewFakeResultCache()
	svc := memory.NewSearchServiceWith(t, bg, []ports.SearchProvider{p}, service.NewCircuitBreaker(clock.Now), func(c *service.Collaborators) { c.ResultCache = cache })

	out1 := runSearch(t, svc, "humble")
	if p.Calls != 1 {
		t.Fatalf("first search: provider calls = %d, want 1", p.Calls)
	}
	if cache.Sets != 1 {
		t.Fatalf("first search: cache sets = %d, want 1 (complete result cached)", cache.Sets)
	}

	out2 := runSearch(t, svc, "humble")
	if p.Calls != 1 {
		t.Fatalf("second search: provider calls = %d, want still 1 (cache hit)", p.Calls)
	}
	if len(out2.Results) != len(out1.Results) || out2.Results[0].Title != out1.Results[0].Title {
		t.Fatalf("cache hit returned different results: %v vs %v", memory.Titles(out1.Results), memory.Titles(out2.Results))
	}
	if out2.Partial {
		t.Error("cache hit must not be marked partial")
	}
}

func TestService_ResultCache_SymbolOnlyQueriesBypassCache(t *testing.T) {
	bg := memory.NewTracker(t)
	p := &memory.QueryFakeProvider{
		Provider: domain.ProviderDeezer,
		ResultsByQuery: map[string][]domain.SearchResult{
			"!!!": {memory.DeezerTrack("Me and Giuliani Down by the School Yard", "!!!", 80)},
			"†††": {memory.DeezerTrack("The Epilogue", "†††", 70)},
		},
	}
	cache := memory.NewFakeResultCache()
	svc := memory.NewSearchServiceWith(t, bg, []ports.SearchProvider{p}, service.NewCircuitBreaker(clock.Now), func(c *service.Collaborators) { c.ResultCache = cache })

	out1 := runSearch(t, svc, "!!!")
	out2 := runSearch(t, svc, "†††")

	if cache.Sets != 0 || cache.Gets != 0 {
		t.Errorf("symbol-only queries must bypass the cache, gets=%d sets=%d", cache.Gets, cache.Sets)
	}
	if len(out1.Results) != 1 || out1.Results[0].Subtitle != "!!!" {
		t.Fatalf("first symbol query got wrong results: %v", memory.Titles(out1.Results))
	}
	if len(out2.Results) != 1 || out2.Results[0].Subtitle != "†††" {
		t.Fatalf("second symbol query served the first query's results: %v", memory.Titles(out2.Results))
	}
}

func TestService_ResultCache_PartialNotCached(t *testing.T) {
	bg := memory.NewTracker(t)
	good := &memory.CountingProvider{Provider: domain.ProviderDeezer, Results: []domain.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	bad := &memory.CountingProvider{Provider: domain.ProviderITunes, Err: errors.New("boom")}
	cache := memory.NewFakeResultCache()
	svc := memory.NewSearchServiceWith(t, bg, []ports.SearchProvider{good, bad}, service.NewCircuitBreaker(clock.Now), func(c *service.Collaborators) { c.ResultCache = cache })

	out := runSearch(t, svc, "humble")
	if !out.Partial {
		t.Fatal("want partial=true when a provider fails")
	}
	if cache.Sets != 0 {
		t.Fatalf("a partial (degraded) result must not be cached, sets = %d", cache.Sets)
	}
}

type fakeProviderCache struct {
	mu    sync.Mutex
	store map[string][]domain.SearchResult
	gets  int
}

func newFakeProviderCache() *fakeProviderCache {
	return &fakeProviderCache{store: map[string][]domain.SearchResult{}}
}

func (c *fakeProviderCache) Get(_ context.Context, provider domain.ProviderName, key string) ([]domain.SearchResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gets++
	r, ok := c.store[provider.String()+"|"+key]
	return slices.Clone(r), ok
}

func (c *fakeProviderCache) Set(_ context.Context, provider domain.ProviderName, key string, results []domain.SearchResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[provider.String()+"|"+key] = slices.Clone(results)
}

func TestService_ProviderCache_ReusesEachProvidersAnswer(t *testing.T) {
	bg := memory.NewTracker(t)
	p := &memory.CountingProvider{Provider: domain.ProviderDeezer, Results: []domain.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := memory.NewSearchServiceWith(t, bg, []ports.SearchProvider{p}, service.NewCircuitBreaker(clock.Now), func(c *service.Collaborators) { c.ProviderCache = newFakeProviderCache() })

	runSearch(t, svc, "humble")
	out := runSearch(t, svc, "humble")

	if p.Calls != 1 {
		t.Fatalf("provider calls = %d, want 1 (second search served from the provider cache)", p.Calls)
	}
	if len(out.Results) != 1 || out.Results[0].Title != "Humble" {
		t.Fatalf("cached answer lost: %v", memory.Titles(out.Results))
	}
	if out.Partial {
		t.Error("an answer from the provider cache must not be partial")
	}
}

func TestService_ProviderCache_OnlyTheFailedProviderIsAskedAgain(t *testing.T) {
	bg := memory.NewTracker(t)
	good := &memory.CountingProvider{Provider: domain.ProviderDeezer, Results: []domain.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	bad := &memory.CountingProvider{Provider: domain.ProviderITunes, Err: errors.New("boom")}
	svc := memory.NewSearchServiceWith(t, bg, []ports.SearchProvider{good, bad}, service.NewCircuitBreaker(clock.Now), func(c *service.Collaborators) { c.ProviderCache = newFakeProviderCache() })

	runSearch(t, svc, "humble")
	runSearch(t, svc, "humble")

	if good.Calls != 1 {
		t.Errorf("good provider calls = %d, want 1", good.Calls)
	}
	if bad.Calls != 2 {
		t.Errorf("failed provider calls = %d, want 2 (errors are never cached)", bad.Calls)
	}
}

func TestService_ProviderCache_EmptyAnswerIsCached(t *testing.T) {
	bg := memory.NewTracker(t)
	p := &memory.CountingProvider{Provider: domain.ProviderDeezer}
	svc := memory.NewSearchServiceWith(t, bg, []ports.SearchProvider{p}, service.NewCircuitBreaker(clock.Now), func(c *service.Collaborators) { c.ProviderCache = newFakeProviderCache() })

	runSearch(t, svc, "qwzxv")
	runSearch(t, svc, "qwzxv")

	if p.Calls != 1 {
		t.Fatalf("provider calls = %d, want 1 (a no-match answer is still an answer)", p.Calls)
	}
}

func TestService_ProviderCache_ServesWhileBreakerIsOpen(t *testing.T) {
	bg := memory.NewTracker(t)
	p := &memory.CountingProvider{Provider: domain.ProviderDeezer, Results: []domain.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	breaker := service.NewCircuitBreaker(clock.Now)
	svc := memory.NewSearchServiceWith(t, bg, []ports.SearchProvider{p}, breaker, func(c *service.Collaborators) { c.ProviderCache = newFakeProviderCache() })

	runSearch(t, svc, "humble")
	openBreaker(t, breaker, domain.ProviderDeezer)
	out := runSearch(t, svc, "humble")

	if len(out.Results) != 1 || out.Partial {
		t.Fatalf("cached answer must be served while the breaker is open: results=%v partial=%v",
			memory.Titles(out.Results), out.Partial)
	}
}

func TestService_ProviderCache_KeyedByQueryAndKinds(t *testing.T) {
	bg := memory.NewTracker(t)
	p := &memory.CountingProvider{Provider: domain.ProviderDeezer, Results: []domain.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := memory.NewSearchServiceWith(t, bg, []ports.SearchProvider{p}, service.NewCircuitBreaker(clock.Now), func(c *service.Collaborators) { c.ProviderCache = newFakeProviderCache() })

	runSearch(t, svc, "humble")
	runSearch(t, svc, "  HUMBLE ")
	runSearch(t, svc, "dna")
	tracksOnly, err := domain.NewSearchQuery("humble", map[domain.ResultKind]bool{domain.ResultKindTrack: true}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Execute(context.Background(), memory.NewUser(), tracksOnly); err != nil {
		t.Fatal(err)
	}

	if p.Calls != 3 {
		t.Fatalf("provider calls = %d, want 3 (case and spaces share a key, another query or kind set does not)", p.Calls)
	}
}

func TestService_ProviderCache_SymbolOnlyQueriesBypassIt(t *testing.T) {
	bg := memory.NewTracker(t)
	p := &memory.CountingProvider{Provider: domain.ProviderDeezer}
	cache := newFakeProviderCache()
	svc := memory.NewSearchServiceWith(t, bg, []ports.SearchProvider{p}, service.NewCircuitBreaker(clock.Now), func(c *service.Collaborators) { c.ProviderCache = cache })

	runSearch(t, svc, "!!!")
	runSearch(t, svc, "†††")

	if cache.gets != 0 || p.Calls != 2 {
		t.Fatalf("symbol-only queries normalize to nothing and must not share a key: gets=%d calls=%d", cache.gets, p.Calls)
	}
}

func TestInspectSearch_BypassesProviderCache(t *testing.T) {
	bg := memory.NewTracker(t)
	p := &memory.CountingProvider{Provider: domain.ProviderDeezer, Results: []domain.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := memory.NewSearchServiceWith(t, bg, []ports.SearchProvider{p}, service.NewCircuitBreaker(clock.Now), func(c *service.Collaborators) { c.ProviderCache = newFakeProviderCache() })

	runSearch(t, svc, "humble")
	q, err := domain.NewSearchQuery("humble", domain.AllKinds(), 10)
	if err != nil {
		t.Fatal(err)
	}
	svc.InspectSearchWithStatuses(context.Background(), q)

	if p.Calls != 2 {
		t.Fatalf("provider calls = %d, want 2 (inspection measures live providers)", p.Calls)
	}
}
