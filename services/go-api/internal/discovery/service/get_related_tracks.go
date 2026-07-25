package service

import (
	"context"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

type GetRelatedTracksService struct {
	providers map[string]ports.RelatedTracksProvider
}

func NewGetRelatedTracksService(providers map[string]ports.RelatedTracksProvider) *GetRelatedTracksService {
	return &GetRelatedTracksService{providers: providers}
}

func (s *GetRelatedTracksService) Execute(ctx context.Context, providerName domain.ProviderName, externalID string, limit int) (*ContentFetchResponse, error) {
	provider, ok := s.providers[providerName.String()]
	if !ok {
		return errorContentResponse(providerName), nil
	}
	results, degraded := fetchProviderResults(ctx, providerName, externalID, "related_tracks.provider_failed",
		func(ctx context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
			return provider.GetRelatedTracks(ctx, pn, id)
		})
	if degraded != nil {
		return degraded, nil
	}
	return okContentResponse(providerName, results, limit), nil
}
