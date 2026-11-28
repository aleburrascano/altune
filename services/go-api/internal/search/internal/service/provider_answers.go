package service

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/shared/music"
	"context"
)

type providerAnswers struct {
	cache ports.ProviderResultCache
	key   string
	keyed bool
}

func newProviderAnswers(cache ports.ProviderResultCache, searchQuery string, kinds domain.ResultKindSet) providerAnswers {
	queryNorm := music.NormalizeForMatch(searchQuery)
	if queryNorm == "" {
		return providerAnswers{cache: cache}
	}
	return providerAnswers{cache: cache, key: queryNorm + "|" + kinds.Key(), keyed: true}
}

func (a providerAnswers) get(ctx context.Context, provider domain.ProviderName) ([]domain.SearchResult, bool) {
	if !a.keyed {
		return nil, false
	}
	return a.cache.Get(ctx, provider, a.key)
}

func (a providerAnswers) set(ctx context.Context, provider domain.ProviderName, results []domain.SearchResult) {
	if !a.keyed {
		return
	}
	a.cache.Set(ctx, provider, a.key, results)
}

func cachedStatus(provider domain.ProviderName, results []domain.SearchResult) domain.ProviderSearchResponse {
	return domain.ProviderSearchResponse{
		Provider:    provider,
		Results:     results,
		Status:      domain.ProviderStatusOK,
		ResultCount: len(results),
	}
}
