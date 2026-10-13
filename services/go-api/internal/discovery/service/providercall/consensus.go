package providercall

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/runloop"
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
	var wg sync.WaitGroup
	for _, p := range providers {
		wg.Add(1)
		go func(p ConsensusProvider) {
			defer wg.Done()
			defer runloop.RecoverContext(ctx, "consensus.provider_panic", "provider", p.Name)
			r := collect(ctx, p)
			mu.Lock()
			out[p.Name] = r
			mu.Unlock()
		}(p)
	}
	wg.Wait()
	return out
}
