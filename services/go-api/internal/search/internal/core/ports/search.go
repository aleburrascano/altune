package ports

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/queuetimeout"
	"context"
	"time"
)

var ErrProviderRateLimitQueueTimeout = queuetimeout.Err

type SearchProvider interface {
	Name() domain.ProviderName
	Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error)
	SupportedKinds() map[domain.ResultKind]bool
}

type SearchTimeLimit interface{ SearchTimeout() time.Duration }

type AlbumContentProvider interface {
	GetAlbumTracks(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)
}

type ArtistContentProvider interface {
	GetArtistTopTracks(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)
	GetArtistAlbums(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)
}

type MusicBrainzDiscographyAnchor interface {
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

type HeldResultsCache interface {
	Get(ctx context.Context, key string) ([]domain.SearchResult, bool)
	Set(ctx context.Context, key string, results []domain.SearchResult)
}
