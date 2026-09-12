package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"altune/go-api/internal/discovery/domain"
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
			status, err := a.doCatalogGet(ctx, token, u, out)
			return struct{}{}, status, err
		})
	return err
}

func (a *AppleMusicAdapter) doCatalogGet(ctx context.Context, token, u string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Origin", appleMusicOrigin)
	req.Header.Set("User-Agent", appleMusicUserAgent)

	resp, err := a.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("http status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return resp.StatusCode, fmt.Errorf("decode catalog response: %w", err)
	}
	return resp.StatusCode, nil
}
