package providers

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/redact"
	"context"
	"fmt"
	"log/slog"
	"net/url"
)

func (a *DeezerAdapter) GetAlbumTracks(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	u := fmt.Sprintf(deezerAPIBaseURL+"/album/%s/tracks?limit=50", url.PathEscape(externalID))
	return a.fetchList(ctx, u, func(item deezerItem) domain.SearchResult {
		return mapDeezerResult(item, domain.ResultKindTrack)
	})
}

func (a *DeezerAdapter) GetArtistTopTracks(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	u := fmt.Sprintf(deezerAPIBaseURL+"/artist/%s/top?limit=10", url.PathEscape(externalID))
	return a.fetchList(ctx, u, func(item deezerItem) domain.SearchResult {
		return mapDeezerResult(item, domain.ResultKindTrack)
	})
}

const deezerMaxDiscographyPages = 5

func (a *DeezerAdapter) GetArtistAlbums(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	return fetchPaged(deezerMaxDiscographyPages,
		func(page int) ([]domain.SearchResult, bool, error) {
			u := fmt.Sprintf(deezerAPIBaseURL+"/artist/%s/albums?limit=100&index=%d",
				url.PathEscape(externalID), page*100)
			var body deezerSearchResponse
			if err := a.getJSON(ctx, u, &body); err != nil {
				return nil, false, err
			}
			items := make([]domain.SearchResult, 0, len(body.Data))
			for _, item := range body.Data {
				items = append(items, mapDeezerResult(item, domain.ResultKindAlbum))
			}
			return items, body.NextPageURL != "", nil
		},
		func(page int, err error) {
			slog.WarnContext(ctx, "deezer.artist_albums_page_failed",
				"provider", domain.ProviderDeezer.String(), "artist", externalID, "page", page, "error", redact.Secrets(err.Error()))
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
