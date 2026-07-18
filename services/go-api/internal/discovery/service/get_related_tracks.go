package service

import (
	"context"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

// GetRelatedTracksService returns a provider's per-track "related" set. It is the
// track-keyed sibling of GetArtistContentService: one provider per name, an
// unknown/failed provider degrades to an empty set (never errors the request), and
// the result is truncated to the requested limit.
type GetRelatedTracksService struct {
	providers map[string]ports.RelatedTracksProvider
}

func NewGetRelatedTracksService(providers map[string]ports.RelatedTracksProvider) *GetRelatedTracksService {
	return &GetRelatedTracksService{providers: providers}
}

func (s *GetRelatedTracksService) Execute(ctx context.Context, providerName, externalID string, limit int) (*ContentFetchResponse, error) {
	provider, ok := s.providers[providerName]
	results, degraded := fetchProviderResults(ctx, providerName, externalID, "related_tracks.provider_failed", ok,
		func(ctx context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
			return provider.GetRelatedTracks(ctx, pn, id)
		})
	if degraded != nil {
		return degraded, nil
	}
	return okContentResponse(providerName, results, limit), nil
}
