package service

import (
	"context"
	"errors"
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
	store      map[string][]domain.SearchResult
	gets, sets int
}

func newFakeResultCache() *fakeResultCache {
	return &fakeResultCache{store: map[string][]domain.SearchResult{}}
}

func (c *fakeResultCache) Get(_ context.Context, key string) ([]domain.SearchResult, bool) {
	c.gets++
	r, ok := c.store[key]
	return r, ok
}

func (c *fakeResultCache) Set(_ context.Context, key string, results []domain.SearchResult) {
	c.sets++
	c.store[key] = results
}

func TestService_ResultCache_HitSkipsProvidersAndIsCrossUser(t *testing.T) {
	p := &countingProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	cache := newFakeResultCache()
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithResultCache(cache))

	// runSearch uses a fresh user each call; the app-wide cache is keyed by query
	// only, so the second (different) user must get the first user's cached list.
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
	// "!!!" and "†††" both normalize to "", so they would share the cache key
	// "|<kinds>" and serve each other's results. Symbol-only queries must skip the
	// result cache entirely — both read and write.
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
