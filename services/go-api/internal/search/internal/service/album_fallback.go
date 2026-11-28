package service

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/redact"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"log/slog"
)

type AlbumFallback interface {
	tracks(ctx context.Context, deezer ports.AlbumContentProvider, requested domain.ProviderName, albumTitle, albumArtist string, limit int) *ContentFetchResponse
}

type NoAlbumFallback struct{}

func (NoAlbumFallback) tracks(context.Context, ports.AlbumContentProvider, domain.ProviderName, string, string, int) *ContentFetchResponse {
	return nil
}

type SearchAlbumFallback struct {
	searcher ports.SearchProvider
	breaker  *CircuitBreaker
}

func NewSearchAlbumFallback(searcher ports.SearchProvider, breaker *CircuitBreaker) (*SearchAlbumFallback, error) {
	if err := requireCollaborators("the search album fallback",
		collaborator{"a search provider", searcher == nil},
		collaborator{"a circuit breaker", breaker == nil},
	); err != nil {
		return nil, err
	}
	return &SearchAlbumFallback{searcher: searcher, breaker: breaker}, nil
}

func albumSearchQuery(albumTitle, albumArtist string) string {
	if albumArtist == "" {
		return albumTitle
	}
	return albumArtist + " " + albumTitle
}

func (f *SearchAlbumFallback) tracks(ctx context.Context, deezer ports.AlbumContentProvider, requested domain.ProviderName, albumTitle, albumArtist string, limit int) *ContentFetchResponse {
	query := albumSearchQuery(albumTitle, albumArtist)
	candidates, err := GuardedFetch(ctx, f.breaker, f.searcher.Name(), func() ([]domain.SearchResult, error) {
		return f.searcher.Search(ctx, query, map[domain.ResultKind]bool{domain.ResultKindAlbum: true})
	})
	if err != nil {
		slog.WarnContext(ctx, "album_tracks.deezer_fallback_failed",
			"requested_provider", requested.String(), "query", query, logging.KeyError, redact.Secrets(err.Error()))
		return nil
	}

	tracks := f.firstMatchingTracklist(ctx, deezer, candidates, albumTitle, albumArtist)
	if len(tracks) == 0 {
		return nil
	}
	resp := FallbackContentResponse(domain.CanonicalContentProvider, requested, tracks, limit)
	slog.InfoContext(ctx, "album_tracks.deezer_fallback_served",
		"requested_provider", requested.String(), "query", query, "tracks", len(resp.Items))
	return resp
}

func isRequestedAlbum(candidate domain.SearchResult, albumTitle, albumArtist string) bool {
	if len(candidate.Sources) == 0 {
		return false
	}
	if textnorm.NormalizeForMatch(candidate.Title) != textnorm.NormalizeForMatch(albumTitle) {
		return false
	}
	wantArtist := textnorm.NormalizeForMatch(albumArtist)
	return wantArtist == "" || textnorm.NormalizeForMatch(candidate.Subtitle) == wantArtist
}

func (f *SearchAlbumFallback) firstMatchingTracklist(ctx context.Context, deezer ports.AlbumContentProvider, candidates []domain.SearchResult, albumTitle, albumArtist string) []domain.SearchResult {
	for _, candidate := range candidates {
		if !isRequestedAlbum(candidate, albumTitle, albumArtist) {
			continue
		}
		deezerAlbumID := candidate.Sources[0].ExternalID
		tracks, err := GuardedFetch(ctx, f.breaker, domain.CanonicalContentProvider, func() ([]domain.SearchResult, error) {
			return deezer.GetAlbumTracks(ctx, domain.CanonicalContentProvider, deezerAlbumID)
		})
		if err != nil || len(tracks) == 0 {
			continue
		}
		return tracks
	}
	return nil
}
