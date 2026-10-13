package discoverytest

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"sync"
)

type CountingProvider struct {
	Provider domain.ProviderName
	Results  []domain.SearchResult
	Err      error
	Calls    int
}

func (p *CountingProvider) Name() domain.ProviderName { return p.Provider }

func (p *CountingProvider) Search(_ context.Context, _ string, _ map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	p.Calls++
	return p.Results, p.Err
}

func (p *CountingProvider) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

func NewFakeResultCache() *FakeResultCache {
	return &FakeResultCache{Store: map[string][]domain.SearchResult{}}
}

type FakeResultCache struct {
	Mu         sync.Mutex
	Store      map[string][]domain.SearchResult
	Gets, Sets int
}

func (c *FakeResultCache) Get(_ context.Context, key string) ([]domain.SearchResult, bool) {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	c.Gets++
	r, ok := c.Store[key]
	return r, ok
}

func (c *FakeResultCache) Set(_ context.Context, key string, results []domain.SearchResult) {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	c.Sets++
	c.Store[key] = results
}
