package discoverytest

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"sync"
)

type FakeArtworkCache struct {
	Mu    sync.Mutex
	Store map[string]string
}

func (c *FakeArtworkCache) Get(_ context.Context, _ domain.ResultKind, title, _, _ string) (string, domain.ProviderKey, bool, error) {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	url, ok := c.Store[title]
	return url, "", ok, nil
}

func (c *FakeArtworkCache) Set(_ context.Context, _ domain.ResultKind, title, _, _, url string, _ domain.ProviderKey, _ ports.ArtworkConfidence) error {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	c.Store[title] = url
	return nil
}
