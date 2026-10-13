package related

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service/providercall"
	"context"
)

type GetRelatedTracksService struct {
	providers map[string]ports.RelatedTracksProvider
	breaker   *providercall.CircuitBreaker
}

type RelatedTracksOption func(*GetRelatedTracksService)

func WithRelatedCircuitBreaker(cb *providercall.CircuitBreaker) RelatedTracksOption {
	return func(s *GetRelatedTracksService) { s.breaker = cb }
}

func NewGetRelatedTracksService(providers map[string]ports.RelatedTracksProvider, opts ...RelatedTracksOption) *GetRelatedTracksService {
	s := &GetRelatedTracksService{providers: providers}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *GetRelatedTracksService) Execute(ctx context.Context, providerName domain.ProviderName, externalID string, limit int) (*providercall.ContentFetchResponse, error) {
	provider, ok := s.providers[providerName.String()]
	if !ok {
		return providercall.UnservedContentResponse(providerName), nil
	}
	results, degraded := providercall.FetchProviderResults(ctx, s.breaker, providerName, externalID, "related_tracks.provider_failed",
		func(ctx context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
			return provider.GetRelatedTracks(ctx, pn, id)
		})
	if degraded != nil {
		return degraded, nil
	}
	return providercall.OkContentResponse(providerName, results, limit), nil
}
