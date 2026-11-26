package soundcloud

import (
	"altune/go-api/internal/search/internal/adapters/providers/providerrequests"
	"altune/go-api/internal/search/internal/adapters/providers/providersearch"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/clock"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
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

var ErrCollaboratorRequired = errors.New("soundcloud api adapter needs its http client and search fallback, pass NoFallback to switch the fallback off")

var errNoFallback = errors.New("soundcloud search has no fallback")

type NoFallback struct{}

func (NoFallback) Search(context.Context, string, map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	return nil, errNoFallback
}

func NewSoundCloudAPIAdapter(client *http.Client, fallback searchFallback, now clock.Func) (*SoundCloudAPIAdapter, error) {
	if client == nil || fallback == nil || now == nil {
		return nil, ErrCollaboratorRequired
	}
	return &SoundCloudAPIAdapter{
		client:   client,
		resolver: newClientIDResolver(client, now),
		fallback: fallback,
		baseURL:  scAPIBaseURL,
	}, nil
}

func (a *SoundCloudAPIAdapter) Name() domain.ProviderName { return domain.ProviderSoundCloud }

func (a *SoundCloudAPIAdapter) SupportedKinds() map[domain.ResultKind]bool {
	return providersearch.AllSearchKinds()
}

func (a *SoundCloudAPIAdapter) SearchTimeout() time.Duration { return scSearchTimeout }

func (a *SoundCloudAPIAdapter) resolveAndFetch(ctx context.Context, fetch func(clientID string) (int, error)) error {
	_, err := providerrequests.WithAuthRetry(ctx, a.resolver.CachedResolver,
		func(_ context.Context, id string) (struct{}, int, error) {
			status, ferr := fetch(id)
			return struct{}{}, status, ferr
		})
	return err
}

func (a *SoundCloudAPIAdapter) getJSON(ctx context.Context, u string, dst any) (int, error) {
	status, body, err := providerrequests.GetBytes(ctx, a.client, u, providerrequests.WithHeader("User-Agent", scUserAgent))
	if err != nil {
		return status, fmt.Errorf("soundcloud api-v2: %w", err)
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return status, fmt.Errorf("decode response: %w", err)
	}
	return status, nil
}

func (*SoundCloudAPIAdapter) ArtworkSource() domain.ProviderKey { return domain.ProviderKeySoundCloud }
