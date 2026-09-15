package ports

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
)

// ErrProviderRateLimitQueueTimeout reports that a provider call gave up waiting
// for its rate-limiter slot: the caller's deadline would pass before the slot
// came up, or the limiter's queue was already full. The request never reached the provider, so it says nothing about
// the provider's health and must not count against its circuit breaker.
// Errors carrying it also match context.DeadlineExceeded.
var ErrProviderRateLimitQueueTimeout = errors.New("provider rate-limit queue timeout")

type SearchProvider interface {
	Name() domain.ProviderName
	Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error)
	SupportedKinds() map[domain.ResultKind]bool
}

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

type ArtistIDResolver interface {
	ResolveArtistID(ctx context.Context, name string) (externalID string, ok bool)
}

type RelatedTracksProvider interface {
	GetRelatedTracks(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)
}
