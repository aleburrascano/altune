package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"altune/go-api/internal/discovery/domain"
)

type SoundCloudAPIAdapter struct {
	client   *http.Client
	resolver *clientIDResolver
	fallback searchFallback
	baseURL  string
}

type searchFallback interface {
	Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error)
}

const (
	scAPIBaseURL         = "https://api-v2.soundcloud.com"
	scSearchLimit        = 20
	scMaxResults         = 40
	scArtistContentLimit = 50
	scRelatedLimit       = 20
	scSearchTimeout      = 3 * time.Second
	scUserAgent          = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

func NewSoundCloudAPIAdapter(client *http.Client, fallback searchFallback) *SoundCloudAPIAdapter {
	return &SoundCloudAPIAdapter{
		client:   client,
		resolver: newClientIDResolver(client),
		fallback: fallback,
		baseURL:  scAPIBaseURL,
	}
}

func (a *SoundCloudAPIAdapter) Name() domain.ProviderName { return domain.ProviderSoundCloud }

func (a *SoundCloudAPIAdapter) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

func (a *SoundCloudAPIAdapter) SearchTimeout() time.Duration { return scSearchTimeout }

func (a *SoundCloudAPIAdapter) resolveAndFetch(ctx context.Context, fetch func(clientID string) (int, error)) error {
	id, err := a.resolver.get(ctx)
	if err != nil {
		return fmt.Errorf("resolve client_id: %w", err)
	}
	status, err := fetch(id)
	if err != nil && isAuthStatus(status) {
		a.resolver.invalidate(id)
		id, err = a.resolver.get(ctx)
		if err != nil {
			return fmt.Errorf("re-resolve client_id: %w", err)
		}
		_, err = fetch(id)
	}
	return err
}

func (a *SoundCloudAPIAdapter) getJSON(ctx context.Context, u string, dst any) (int, error) {
	status, body, err := getBytes(ctx, a.client, u, withHeader("User-Agent", scUserAgent))
	if err != nil {
		return status, fmt.Errorf("soundcloud api-v2: %w", err)
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return status, fmt.Errorf("decode response: %w", err)
	}
	return status, nil
}

func (*SoundCloudAPIAdapter) ArtworkSource() string { return "soundcloud" }
