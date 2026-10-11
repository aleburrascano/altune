package ports

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"time"
)

var ErrProviderRateLimitQueueTimeout = errors.New("provider rate-limit queue timeout")

type SearchProvider interface {
	Name() domain.ProviderName
	Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error)
	SupportedKinds() map[domain.ResultKind]bool
}

type SearchTimeouter interface{ SearchTimeout() time.Duration }

type AlbumContentProvider interface {
	GetAlbumTracks(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)
}

type ArtistContentProvider interface {
	GetArtistTopTracks(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)
	GetArtistAlbums(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)
}

type MBDiscographyAnchor interface {
	ReleaseGroupTitles(ctx context.Context, mbid string) ([]string, error)
}

type RelatedTracksProvider interface {
	GetRelatedTracks(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)
}

type ResultCache interface {
	Get(ctx context.Context, key string) ([]domain.SearchResult, bool)
	Set(ctx context.Context, key string, results []domain.SearchResult)
}

type ProviderResultCache interface {
	Get(ctx context.Context, provider domain.ProviderName, key string) ([]domain.SearchResult, bool)
	Set(ctx context.Context, provider domain.ProviderName, key string, results []domain.SearchResult)
}

type HeldSlateCache interface {
	Get(ctx context.Context, key string) ([]domain.SearchResult, bool)
	Set(ctx context.Context, key string, results []domain.SearchResult)
}
