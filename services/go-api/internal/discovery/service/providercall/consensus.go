package providercall

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/fanout"
	"context"
	"sync"
)

type ConsensusProvider struct {
	Name     string
	Provider domain.ProviderName
	Fetcher  func(ctx context.Context, artistName string) ([]domain.SearchResult, error)
}

func FanOutConsensus[T any](
	ctx context.Context,
	providers []ConsensusProvider,
	collect func(ctx context.Context, p ConsensusProvider) T,
) map[string]T {
	var mu sync.Mutex
	out := make(map[string]T, len(providers))
	g := fanout.New("consensus.provider_panic", len(providers))
	for _, p := range providers {
		g.Go(ctx, func() {
			r := collect(ctx, p)
			mu.Lock()
			defer mu.Unlock()
			out[p.Name] = r
		}, "provider", p.Name)
	}
	_ = g.Wait()
	return out
}
