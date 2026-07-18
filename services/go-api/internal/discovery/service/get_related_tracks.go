package service

import (
	"context"
	"log/slog"

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
	if !ok {
		return errorContentResponse(providerName), nil
	}

	pn, err := domain.ParseProviderName(providerName)
	if err != nil {
		return errorContentResponse(providerName), nil
	}

	results, err := provider.GetRelatedTracks(ctx, pn, externalID)
	if err != nil {
		slog.WarnContext(ctx, "related_tracks.provider_failed",
			"provider", providerName, "external_id", externalID, "error", err)
		return errorContentResponse(providerName), nil
	}

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return &ContentFetchResponse{
		ProviderName: providerName,
		Status:       domain.ProviderStatusOK,
		Items:        results,
	}, nil
}
