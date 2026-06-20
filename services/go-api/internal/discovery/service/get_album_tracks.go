package service

import (
	"context"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

type GetAlbumTracksService struct {
	providers map[string]ports.AlbumContentProvider
}

func NewGetAlbumTracksService(providers map[string]ports.AlbumContentProvider) *GetAlbumTracksService {
	return &GetAlbumTracksService{providers: providers}
}

type ContentFetchResponse struct {
	ProviderName string
	Status       domain.ProviderStatus
	Items        []domain.SearchResult
}

func (s *GetAlbumTracksService) Execute(ctx context.Context, providerName, externalID string, limit int) (*ContentFetchResponse, error) {
	provider, ok := s.providers[providerName]
	if !ok {
		return &ContentFetchResponse{
			ProviderName: providerName,
			Status:       domain.ProviderStatusError,
			Items:        []domain.SearchResult{},
		}, nil
	}

	pn, err := domain.ParseProviderName(providerName)
	if err != nil {
		return &ContentFetchResponse{
			ProviderName: providerName,
			Status:       domain.ProviderStatusError,
			Items:        []domain.SearchResult{},
		}, nil
	}
	results, err := provider.GetAlbumTracks(ctx, pn, externalID)
	if err != nil {
		return &ContentFetchResponse{
			ProviderName: providerName,
			Status:       domain.ProviderStatusError,
			Items:        []domain.SearchResult{},
		}, nil
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
