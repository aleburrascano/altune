package deezer

import (
	"altune/go-api/internal/discovery/adapters/providers/providerhttp"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/redact"
	"context"
	"fmt"
	"log/slog"
	"net/url"
)

const deezerMaxAlbumTrackPages = 5

func (a *DeezerAdapter) GetAlbumTracks(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	return a.fetchAllPages(ctx, "/album/%s/tracks", externalID, domain.ResultKindTrack, deezerMaxAlbumTrackPages, "deezer.album_tracks_page_failed")
}

func (a *DeezerAdapter) GetArtistTopTracks(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	u := fmt.Sprintf(deezerAPIBaseURL+"/artist/%s/top?limit=10", url.PathEscape(externalID))
	return a.fetchList(ctx, u, func(item deezerItem) domain.SearchResult {
		return mapDeezerResult(item, domain.ResultKindTrack)
	})
}

const (
	deezerMaxDiscographyPages = 5
	deezerPageSize            = 100
)

func (a *DeezerAdapter) GetArtistAlbums(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	return a.fetchAllPages(ctx, "/artist/%s/albums", externalID, domain.ResultKindAlbum, deezerMaxDiscographyPages, "deezer.artist_albums_page_failed")
}

func (a *DeezerAdapter) fetchAllPages(ctx context.Context, pathFormat, externalID string, kind domain.ResultKind, maxPages int, failedEvent string) ([]domain.SearchResult, error) {
	return providerhttp.FetchPaged(maxPages,
		func(page int) ([]domain.SearchResult, bool, error) {
			u := fmt.Sprintf(deezerAPIBaseURL+pathFormat+"?limit=%d&index=%d",
				url.PathEscape(externalID), deezerPageSize, page*deezerPageSize)
			var body deezerSearchResponse
			if err := a.getJSON(ctx, u, &body); err != nil {
				return nil, false, err
			}
			items := make([]domain.SearchResult, 0, len(body.Data))
			for _, item := range body.Data {
				items = append(items, mapDeezerResult(item, kind))
			}
			return items, body.NextPageURL != "", nil
		},
		func(page int, err error) {
			slog.WarnContext(ctx, failedEvent,
				"provider", domain.ProviderDeezer.String(), "id", externalID, "page", page, "error", redact.Secrets(err.Error()))
		})
}

func (a *DeezerAdapter) fetchList(ctx context.Context, u string, mapper func(deezerItem) domain.SearchResult) ([]domain.SearchResult, error) {
	var body deezerSearchResponse
	if err := a.getJSON(ctx, u, &body); err != nil {
		return nil, err
	}

	var results []domain.SearchResult
	for _, item := range body.Data {
		results = append(results, mapper(item))
	}
	return results, nil
}
