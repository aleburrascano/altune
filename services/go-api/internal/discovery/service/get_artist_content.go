package service

import (
	"context"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

type GetArtistContentService struct {
	providers map[string]ports.ArtistContentProvider
}

func NewGetArtistContentService(providers map[string]ports.ArtistContentProvider) *GetArtistContentService {
	return &GetArtistContentService{providers: providers}
}

func (s *GetArtistContentService) GetTopTracks(ctx context.Context, providerName, externalID string, limit int) (*ContentFetchResponse, error) {
	provider, ok := s.providers[providerName]
	if !ok {
		return &ContentFetchResponse{
			ProviderName: providerName,
			Status:       domain.ProviderStatusError,
		}, nil
	}

	pn, _ := domain.ParseProviderName(providerName)
	results, err := provider.GetArtistTopTracks(ctx, pn, externalID)
	if err != nil {
		return &ContentFetchResponse{
			ProviderName: providerName,
			Status:       domain.ProviderStatusError,
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

func (s *GetArtistContentService) GetAlbums(ctx context.Context, providerName, externalID string, limit int) (*ContentFetchResponse, error) {
	provider, ok := s.providers[providerName]
	if !ok {
		return &ContentFetchResponse{
			ProviderName: providerName,
			Status:       domain.ProviderStatusError,
		}, nil
	}

	pn, _ := domain.ParseProviderName(providerName)
	results, err := provider.GetArtistAlbums(ctx, pn, externalID)
	if err != nil {
		return &ContentFetchResponse{
			ProviderName: providerName,
			Status:       domain.ProviderStatusError,
		}, nil
	}

	results = dedupAlbums(results)

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return &ContentFetchResponse{
		ProviderName: providerName,
		Status:       domain.ProviderStatusOK,
		Items:        results,
	}, nil
}

func dedupAlbums(results []domain.SearchResult) []domain.SearchResult {
	seen := make(map[string]int)
	var deduped []domain.SearchResult

	for _, r := range results {
		normTitle := NormalizeForMatch(r.Title)
		if idx, ok := seen[normTitle]; ok {
			existingCount := getTrackCount(deduped[idx])
			newCount := getTrackCount(r)
			if newCount > existingCount {
				deduped[idx] = r
			}
			continue
		}
		seen[normTitle] = len(deduped)
		deduped = append(deduped, r)
	}
	return deduped
}

func getTrackCount(r domain.SearchResult) int {
	if r.Extras == nil {
		return 0
	}
	v, ok := r.Extras["track_count"]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}
