package service

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

type countingProvider struct {
	name    domain.ProviderName
	results []domain.SearchResult
	err     error
	calls   int
}

func (p *countingProvider) Name() domain.ProviderName { return p.name }

func (p *countingProvider) Search(_ context.Context, _ string, _ map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	p.calls++
	return p.results, p.err
}

func (p *countingProvider) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

type fakeResultCache struct {
	mu         sync.Mutex
	store      map[string][]domain.SearchResult
	gets, sets int
}

func newFakeResultCache() *fakeResultCache {
	return &fakeResultCache{store: map[string][]domain.SearchResult{}}
}

func (c *fakeResultCache) Get(_ context.Context, key string) ([]domain.SearchResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gets++
	r, ok := c.store[key]
	return r, ok
}

func (c *fakeResultCache) Set(_ context.Context, key string, results []domain.SearchResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sets++
	c.store[key] = results
}

func TestService_ResultCache_HitSkipsProvidersAndIsCrossUser(t *testing.T) {
	p := &countingProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	cache := newFakeResultCache()
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithResultCache(cache))

	out1 := runSearch(t, svc, "humble")
	if p.calls != 1 {
		t.Fatalf("first search: provider calls = %d, want 1", p.calls)
	}
	if cache.sets != 1 {
		t.Fatalf("first search: cache sets = %d, want 1 (complete result cached)", cache.sets)
	}

	out2 := runSearch(t, svc, "humble")
	if p.calls != 1 {
		t.Fatalf("second search: provider calls = %d, want still 1 (cache hit)", p.calls)
	}
	if len(out2.Results) != len(out1.Results) || out2.Results[0].Title != out1.Results[0].Title {
		t.Fatalf("cache hit returned different results: %v vs %v", titles(out1.Results), titles(out2.Results))
	}
	if out2.Partial {
		t.Error("cache hit must not be marked partial")
	}
}

func TestService_ResultCache_SymbolOnlyQueriesBypassCache(t *testing.T) {
	p := &queryFakeProvider{
		name: domain.ProviderDeezer,
		resultsByQuery: map[string][]domain.SearchResult{
			"!!!": {deezerTrack("Me and Giuliani Down by the School Yard", "!!!", 80)},
			"†††": {deezerTrack("The Epilogue", "†††", 70)},
		},
	}
	cache := newFakeResultCache()
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithResultCache(cache))

	out1 := runSearch(t, svc, "!!!")
	out2 := runSearch(t, svc, "†††")

	if cache.sets != 0 || cache.gets != 0 {
		t.Errorf("symbol-only queries must bypass the cache, gets=%d sets=%d", cache.gets, cache.sets)
	}
	if len(out1.Results) != 1 || out1.Results[0].Subtitle != "!!!" {
		t.Fatalf("first symbol query got wrong results: %v", titles(out1.Results))
	}
	if len(out2.Results) != 1 || out2.Results[0].Subtitle != "†††" {
		t.Fatalf("second symbol query served the first query's results: %v", titles(out2.Results))
	}
}

func TestService_ResultCache_PartialNotCached(t *testing.T) {
	good := &countingProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	bad := &countingProvider{name: domain.ProviderITunes, err: errors.New("boom")}
	cache := newFakeResultCache()
	svc := NewService([]ports.SearchProvider{good, bad}, NewCircuitBreaker(), WithResultCache(cache))

	out := runSearch(t, svc, "humble")
	if !out.Partial {
		t.Fatal("want partial=true when a provider fails")
	}
	if cache.sets != 0 {
		t.Fatalf("a partial (degraded) result must not be cached, sets = %d", cache.sets)
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
	p := &countingProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithProviderResultCache(newFakeProviderCache()))

	runSearch(t, svc, "humble")
	out := runSearch(t, svc, "humble")

	if p.calls != 1 {
		t.Fatalf("provider calls = %d, want 1 (second search served from the provider cache)", p.calls)
	}
	if len(out.Results) != 1 || out.Results[0].Title != "Humble" {
		t.Fatalf("cached answer lost: %v", titles(out.Results))
	}
	if out.Partial {
		t.Error("an answer from the provider cache must not be partial")
	}
}

func TestService_ProviderCache_OnlyTheFailedProviderIsAskedAgain(t *testing.T) {
	good := &countingProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	bad := &countingProvider{name: domain.ProviderITunes, err: errors.New("boom")}
	svc := NewService([]ports.SearchProvider{good, bad}, NewCircuitBreaker(), WithProviderResultCache(newFakeProviderCache()))

	runSearch(t, svc, "humble")
	runSearch(t, svc, "humble")

	if good.calls != 1 {
		t.Errorf("good provider calls = %d, want 1", good.calls)
	}
	if bad.calls != 2 {
		t.Errorf("failed provider calls = %d, want 2 (errors are never cached)", bad.calls)
	}
}

func TestService_ProviderCache_EmptyAnswerIsCached(t *testing.T) {
	p := &countingProvider{name: domain.ProviderDeezer}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithProviderResultCache(newFakeProviderCache()))

	runSearch(t, svc, "qwzxv")
	runSearch(t, svc, "qwzxv")

	if p.calls != 1 {
		t.Fatalf("provider calls = %d, want 1 (a no-match answer is still an answer)", p.calls)
	}
}

func TestService_ProviderCache_ServesWhileBreakerIsOpen(t *testing.T) {
	p := &countingProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	breaker := NewCircuitBreaker()
	svc := NewService([]ports.SearchProvider{p}, breaker, WithProviderResultCache(newFakeProviderCache()))

	runSearch(t, svc, "humble")
	for range failureThreshold {
		breaker.RecordFailure(context.Background(), domain.ProviderDeezer)
	}
	out := runSearch(t, svc, "humble")

	if len(out.Results) != 1 || out.Partial {
		t.Fatalf("cached answer must be served while the breaker is open: results=%v partial=%v",
			titles(out.Results), out.Partial)
	}
}

func TestService_ProviderCache_KeyedByQueryAndKinds(t *testing.T) {
	p := &countingProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithProviderResultCache(newFakeProviderCache()))

	runSearch(t, svc, "humble")
	runSearch(t, svc, "  HUMBLE ")
	runSearch(t, svc, "dna")
	tracksOnly, err := domain.NewSearchQuery("humble", map[domain.ResultKind]bool{domain.ResultKindTrack: true}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Execute(context.Background(), newUser(), tracksOnly, false); err != nil {
		t.Fatal(err)
	}

	if p.calls != 3 {
		t.Fatalf("provider calls = %d, want 3 (case and spaces share a key, another query or kind set does not)", p.calls)
	}
}

func TestService_ProviderCache_SymbolOnlyQueriesBypassIt(t *testing.T) {
	p := &countingProvider{name: domain.ProviderDeezer}
	cache := newFakeProviderCache()
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithProviderResultCache(cache))

	runSearch(t, svc, "!!!")
	runSearch(t, svc, "†††")

	if cache.gets != 0 || p.calls != 2 {
		t.Fatalf("symbol-only queries normalize to nothing and must not share a key: gets=%d calls=%d", cache.gets, p.calls)
	}
}

func TestInspectSearch_BypassesProviderCache(t *testing.T) {
	p := &countingProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithProviderResultCache(newFakeProviderCache()))

	runSearch(t, svc, "humble")
	q, err := domain.NewSearchQuery("humble", domain.AllKinds(), 10)
	if err != nil {
		t.Fatal(err)
	}
	svc.InspectSearchWithStatuses(context.Background(), q)

	if p.calls != 2 {
		t.Fatalf("provider calls = %d, want 2 (inspection measures live providers)", p.calls)
	}
}
