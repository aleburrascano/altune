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

// StructuredSearcher is an optional interface that providers can implement
// to receive artist+track split queries instead of raw strings.
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

// ArtistIDResolver resolves an artist NAME to this provider's own artist id. It
// exists for providers MusicBrainz's url-relations never bridge (SoundCloud), so
// the identity-first content fan-out can still reach them: resolve the name once
// to a single artist id, then query by that id (never per-item names). Optional
// capability, probed by type-assertion; only SoundCloud implements it today
// (searchArtists top match), mirroring RelatedTracksProvider.
type ArtistIDResolver interface {
	ResolveArtistID(ctx context.Context, name string) (externalID string, ok bool)
}

// RelatedTracksProvider returns a provider's per-track "related" recommendation
// set, keyed by the track's external id. Track-keyed sibling of
// ArtistContentProvider; only SoundCloud implements it today
// (/tracks/{id}/related).
type RelatedTracksProvider interface {
	GetRelatedTracks(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)
}

