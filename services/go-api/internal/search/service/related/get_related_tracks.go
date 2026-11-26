package related

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/service/providerguard"
	"context"
)

type GetRelatedTracksService struct {
	providers map[string]ports.RelatedTracksProvider
	breaker   *providerguard.CircuitBreaker
}

type RelatedTracksOption func(*GetRelatedTracksService)

func WithRelatedCircuitBreaker(cb *providerguard.CircuitBreaker) RelatedTracksOption {
	return func(s *GetRelatedTracksService) { s.breaker = cb }
}

func NewGetRelatedTracksService(providers map[string]ports.RelatedTracksProvider, opts ...RelatedTracksOption) *GetRelatedTracksService {
	s := &GetRelatedTracksService{providers: providers}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *GetRelatedTracksService) Execute(ctx context.Context, providerName domain.ProviderName, externalID string, limit int) (*providerguard.ContentFetchResponse, error) {
	provider, ok := s.providers[providerName.String()]
	if !ok {
		return providerguard.UnservedContentResponse(providerName), nil
	}
	results, degraded := providerguard.FetchProviderResults(ctx, s.breaker, providerName, externalID, "related_tracks.provider_failed",
		func(ctx context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
			return provider.GetRelatedTracks(ctx, pn, id)
		})
	if degraded != nil {
		return degraded, nil
	}
	return providerguard.OkContentResponse(providerName, results, limit), nil
}
