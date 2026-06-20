package service

import (
	"context"
	"log/slog"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

type GetArtistContentService struct {
	providers map[string]ports.ArtistContentProvider
	consensus *ConsensusService
}

func NewGetArtistContentService(
	providers map[string]ports.ArtistContentProvider,
	opts ...ArtistContentOption,
) *GetArtistContentService {
	s := &GetArtistContentService{providers: providers}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

type ArtistContentOption func(*GetArtistContentService)

func WithConsensusService(c *ConsensusService) ArtistContentOption {
	return func(s *GetArtistContentService) { s.consensus = c }
}

func (s *GetArtistContentService) GetTopTracks(ctx context.Context, providerName, externalID string, limit int) (*ContentFetchResponse, error) {
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
	results, err := provider.GetArtistTopTracks(ctx, pn, externalID)
	if err != nil {
		slog.WarnContext(ctx, "artist_top_tracks.provider_failed",
			"provider", providerName, "external_id", externalID, "error", err)
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

func (s *GetArtistContentService) GetAlbums(ctx context.Context, providerName, externalID, artistName string, limit int) (*ContentFetchResponse, error) {
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
	results, err := provider.GetArtistAlbums(ctx, pn, externalID)
	if err != nil {
		slog.WarnContext(ctx, "artist_albums.provider_failed",
			"provider", providerName, "external_id", externalID, "error", err)
		return &ContentFetchResponse{
			ProviderName: providerName,
			Status:       domain.ProviderStatusError,
			Items:        []domain.SearchResult{},
		}, nil
	}

	results = dedupAlbums(results)

	if artistName != "" && s.consensus != nil {
		consensusResults := s.consensus.BuildConsensus(ctx, artistName, results)
		var kept []domain.SearchResult
		for _, cr := range consensusResults {
			if cr.Status != ConsensusRejected {
				kept = append(kept, cr.Album)
			}
		}
		if kept == nil {
			kept = []domain.SearchResult{}
		}
		results = kept
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

func dedupAlbums(results []domain.SearchResult) []domain.SearchResult {
	seen := make(map[string]int)
	var deduped []domain.SearchResult

	for _, r := range results {
		normTitle := NormalizeForMatch(r.Title) + "|" + NormalizeForMatch(r.Subtitle)
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
