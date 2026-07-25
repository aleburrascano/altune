package ports

import (
	"context"
	"time"

	"altune/go-api/internal/discovery/domain"
)

type SearchProvider interface {
	Name() domain.ProviderName
	Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error)
	SupportedKinds() map[domain.ResultKind]bool
}

type StructuredSearcher interface {
	SearchStructured(ctx context.Context, artist, track string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error)
}

type QueryCache interface {
	Get(ctx context.Context, provider domain.ProviderName, kindsCSV, queryHash string) ([]domain.SearchResult, time.Time, bool, error)
	Set(ctx context.Context, provider domain.ProviderName, kindsCSV, queryHash string, results []domain.SearchResult) error
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
