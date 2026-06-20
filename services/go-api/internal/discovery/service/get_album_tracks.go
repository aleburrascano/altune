package service

import (
	"context"
	"log/slog"

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

func (s *GetAlbumTracksService) Execute(ctx context.Context, providerName, externalID, albumTitle, albumArtist string, limit int) (*ContentFetchResponse, error) {
	provider, ok := s.providers[providerName]
	if !ok {
		// Fallback: if the requested provider isn't supported but Deezer is,
		// search Deezer for this album by title+artist and return those tracks.
		deezer, hasDeezer := s.providers["deezer"]
		if hasDeezer && albumTitle != "" {
			return s.deezerSearchFallback(ctx, deezer, albumTitle, albumArtist, limit)
		}
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
		slog.WarnContext(ctx, "album_tracks.provider_failed",
			"provider", providerName, "external_id", externalID, "error", err)
	}
	if err != nil || len(results) == 0 {
		if albumTitle != "" {
			deezer, hasDeezer := s.providers["deezer"]
			if hasDeezer {
				return s.deezerSearchFallback(ctx, deezer, albumTitle, albumArtist, limit)
			}
		}
		if err != nil {
			return &ContentFetchResponse{
				ProviderName: providerName,
				Status:       domain.ProviderStatusError,
				Items:        []domain.SearchResult{},
			}, nil
		}
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

func (s *GetAlbumTracksService) deezerSearchFallback(ctx context.Context, deezer ports.AlbumContentProvider, albumTitle, albumArtist string, limit int) (*ContentFetchResponse, error) {
	searcher, ok := deezer.(ports.SearchProvider)
	if !ok {
		return &ContentFetchResponse{
			ProviderName: "deezer",
			Status:       domain.ProviderStatusError,
			Items:        []domain.SearchResult{},
		}, nil
	}

	query := albumTitle
	if albumArtist != "" {
		query = albumArtist + " " + albumTitle
	}

	results, err := searcher.Search(ctx, query, map[domain.ResultKind]bool{domain.ResultKindAlbum: true})
	if err != nil {
		slog.WarnContext(ctx, "album_tracks.deezer_fallback_failed",
			"query", query, "error", err)
	}
	if err != nil || len(results) == 0 {
		return &ContentFetchResponse{
			ProviderName: "deezer",
			Status:       domain.ProviderStatusOK,
			Items:        []domain.SearchResult{},
		}, nil
	}

	// Use the first matching album's Deezer ID to fetch tracks
	for _, r := range results {
		if len(r.Sources) == 0 {
			continue
		}
		deezerAlbumID := r.Sources[0].ExternalID
		tracks, err := deezer.GetAlbumTracks(ctx, domain.ProviderDeezer, deezerAlbumID)
		if err != nil || len(tracks) == 0 {
			continue
		}
		if limit > 0 && len(tracks) > limit {
			tracks = tracks[:limit]
		}
		return &ContentFetchResponse{
			ProviderName: "deezer",
			Status:       domain.ProviderStatusOK,
			Items:        tracks,
		}, nil
	}

	return &ContentFetchResponse{
		ProviderName: "deezer",
		Status:       domain.ProviderStatusOK,
		Items:        []domain.SearchResult{},
	}, nil
}
