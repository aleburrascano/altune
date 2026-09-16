package providers

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"fmt"
	"net/url"
)

const appleMusicContentLimit = 25

func (a *AppleMusicAdapter) GetArtistAlbums(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	u := fmt.Sprintf("%s/artists/%s/albums?limit=%d", a.catalogBase, url.PathEscape(externalID), appleMusicContentLimit)
	var body appleMusicResultGroup[appleMusicAlbum]
	if err := a.fetchCatalog(ctx, u, &body); err != nil {
		return nil, err
	}
	out := make([]domain.SearchResult, 0, len(body.Data))
	for _, al := range body.Data {
		out = append(out, mapAppleMusicAlbum(al))
	}
	return out, nil
}

func (a *AppleMusicAdapter) GetArtistTopTracks(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	u := fmt.Sprintf("%s/artists/%s/view/top-songs?limit=%d", a.catalogBase, url.PathEscape(externalID), appleMusicContentLimit)
	var body appleMusicResultGroup[appleMusicSong]
	if err := a.fetchCatalog(ctx, u, &body); err != nil {
		return nil, err
	}
	out := make([]domain.SearchResult, 0, len(body.Data))
	for _, s := range body.Data {
		out = append(out, mapAppleMusicSong(s))
	}
	return out, nil
}

func (a *AppleMusicAdapter) fetchCatalog(ctx context.Context, u string, out any) error {
	_, err := withAuthRetry(ctx, a.resolver.cachedResolver,
		func(ctx context.Context, token string) (struct{}, int, error) {
			status, err := a.getCatalogJSON(ctx, token, u, out, "catalog response")
			return struct{}{}, status, err
		})
	return err
}
