package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
	"context"
)

type providerAnswers struct {
	cache ports.ProviderResultCache
	key   string
}

func newProviderAnswers(cache ports.ProviderResultCache, searchQuery string, kinds domain.ResultKindSet) providerAnswers {
	queryNorm := textnorm.NormalizeForMatch(searchQuery)
	if cache == nil || queryNorm == "" {
		return providerAnswers{}
	}
	return providerAnswers{cache: cache, key: queryNorm + "|" + kinds.Key()}
}

func (a providerAnswers) get(ctx context.Context, provider domain.ProviderName) ([]domain.SearchResult, bool) {
	if a.cache == nil {
		return nil, false
	}
	return a.cache.Get(ctx, provider, a.key)
}

func (a providerAnswers) set(ctx context.Context, provider domain.ProviderName, results []domain.SearchResult) {
	if a.cache == nil {
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
