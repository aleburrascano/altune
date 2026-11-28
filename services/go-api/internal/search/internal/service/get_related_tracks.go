package service

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"context"
)

type GetRelatedTracksService struct {
	providers map[string]ports.RelatedTracksProvider
	breaker   *CircuitBreaker
}

func NewGetRelatedTracksService(providers map[string]ports.RelatedTracksProvider, breaker *CircuitBreaker) (*GetRelatedTracksService, error) {
	if providers == nil {
		return nil, required("providers")
	}
	if breaker == nil {
		return nil, required("breaker")
	}
	return &GetRelatedTracksService{providers: providers, breaker: breaker}, nil
}

func (s *GetRelatedTracksService) Execute(ctx context.Context, providerName domain.ProviderName, externalID string, limit int) (*ContentFetchResponse, error) {
	provider, ok := s.providers[providerName.String()]
	if !ok {
		return UnservedContentResponse(providerName), nil
	}
	results, degraded := FetchProviderResults(ctx, s.breaker, providerName, externalID, "related_tracks.provider_failed",
		func(ctx context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
			return provider.GetRelatedTracks(ctx, pn, id)
		})
	if degraded != nil {
		return degraded, nil
	}
	return OkContentResponse(providerName, results, limit), nil
}
