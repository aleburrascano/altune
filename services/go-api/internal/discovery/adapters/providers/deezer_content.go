package providers

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"fmt"
	"log/slog"
	"net/url"
)

func (a *DeezerAdapter) GetAlbumTracks(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	u := fmt.Sprintf("https://api.deezer.com/album/%s/tracks?limit=50", url.PathEscape(externalID))
	return a.fetchList(ctx, u, func(item deezerItem) domain.SearchResult {
		return mapDeezerResult(item, domain.ResultKindTrack)
	})
}

func (a *DeezerAdapter) GetArtistTopTracks(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	u := fmt.Sprintf("https://api.deezer.com/artist/%s/top?limit=10", url.PathEscape(externalID))
	return a.fetchList(ctx, u, func(item deezerItem) domain.SearchResult {
		return mapDeezerResult(item, domain.ResultKindTrack)
	})
}

const deezerMaxDiscographyPages = 5

func (a *DeezerAdapter) GetArtistAlbums(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	return fetchPaged(deezerMaxDiscographyPages,
		func(page int) ([]domain.SearchResult, bool, error) {
			u := fmt.Sprintf("https://api.deezer.com/artist/%s/albums?limit=100&index=%d",
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
			slog.DebugContext(ctx, "deezer.artist_albums_page_failed",
				"artist", externalID, "page", page, "error", err)
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

func (a *DeezerAdapter) FetchTrackISRC(ctx context.Context, trackID string) (string, error) {
	u := fmt.Sprintf("https://api.deezer.com/track/%s", url.PathEscape(trackID))
	var detail struct {
		ISRC string `json:"isrc"`
	}
	if err := a.getJSON(ctx, u, &detail); err != nil {
		return "", nil //nolint:nilerr // intentional graceful degradation: missing ISRC is non-fatal
	}
	return detail.ISRC, nil
}

func (a *DeezerAdapter) FetchFirstTrackID(ctx context.Context, albumID string) (string, error) {
	u := fmt.Sprintf("https://api.deezer.com/album/%s/tracks?limit=1", url.PathEscape(albumID))
	var body struct {
		Data []struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	if err := a.getJSON(ctx, u, &body); err != nil {
		return "", nil //nolint:nilerr // intentional graceful degradation: missing track id is non-fatal
	}
	if len(body.Data) == 0 {
		return "", nil
	}
	return fmt.Sprintf("%d", body.Data[0].ID), nil
}
