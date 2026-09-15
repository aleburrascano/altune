package service

import (
	"context"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

type GetRelatedTracksService struct {
	providers map[string]ports.RelatedTracksProvider
	breaker   *CircuitBreaker
}

type RelatedTracksOption func(*GetRelatedTracksService)

// WithRelatedCircuitBreaker gates every provider call the service makes through
// cb, the breaker shared with the search fan-out. Without it, calls are ungated.
func WithRelatedCircuitBreaker(cb *CircuitBreaker) RelatedTracksOption {
	return func(s *GetRelatedTracksService) { s.breaker = cb }
}

func NewGetRelatedTracksService(providers map[string]ports.RelatedTracksProvider, opts ...RelatedTracksOption) *GetRelatedTracksService {
	s := &GetRelatedTracksService{providers: providers}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *GetRelatedTracksService) Execute(ctx context.Context, providerName domain.ProviderName, externalID string, limit int) (*ContentFetchResponse, error) {
	provider, ok := s.providers[providerName.String()]
	if !ok {
		return unservedContentResponse(providerName), nil
	}
	results, degraded := fetchProviderResults(ctx, s.breaker, providerName, externalID, "related_tracks.provider_failed",
		func(ctx context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
			return provider.GetRelatedTracks(ctx, pn, id)
		})
	if degraded != nil {
		return degraded, nil
	}
	return okContentResponse(providerName, results, limit), nil
}
