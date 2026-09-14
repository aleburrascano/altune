package providers

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"fmt"
	"net/url"
)

func (a *ITunesAdapter) GetAlbumTracks(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	return a.lookupContent(ctx, externalID, "song")
}

func (a *ITunesAdapter) GetArtistTopTracks(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	return a.lookupContent(ctx, externalID, "song")
}

func (a *ITunesAdapter) GetArtistAlbums(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	return a.lookupContent(ctx, externalID, "album")
}

func (a *ITunesAdapter) lookupContent(ctx context.Context, id, entity string) ([]domain.SearchResult, error) {
	u := fmt.Sprintf(
		"https://itunes.apple.com/lookup?id=%s&entity=%s&country=US&limit=50",
		url.QueryEscape(id), entity,
	)
	a.rateLimit(ctx)
	var body itunesResponse
	if err := getJSON(ctx, a.client, u, &body, withHeader("User-Agent", itunesUserAgent)); err != nil {
		return nil, err
	}

	targetWrapper, kind := itunesContentTarget(entity)
	results := make([]domain.SearchResult, 0, len(body.Results))
	for _, item := range body.Results {
		if item.WrapperType != targetWrapper {
			continue
		}
		results = append(results, mapITunesResult(item, kind))
	}
	return results, nil
}

func itunesContentTarget(entity string) (wrapperType string, kind domain.ResultKind) {
	if entity == "album" {
		return "collection", domain.ResultKindAlbum
	}
	return "track", domain.ResultKindTrack
}
