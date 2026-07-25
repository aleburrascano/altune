package ports

import (
	"context"

	"altune/go-api/internal/discovery/domain"
)

type MetadataEnricher interface {
	ResolveMBID(ctx context.Context, kind domain.ResultKind, title, subtitle string) (string, error)
	Lookup(ctx context.Context, kind domain.ResultKind, mbid string) (domain.MBEnrichment, error)
}

type EnrichmentCache interface {
	Get(ctx context.Context, kind domain.ResultKind, mbid string) (domain.MBEnrichment, bool, error)
	Set(ctx context.Context, kind domain.ResultKind, mbid string, e domain.MBEnrichment) error
	GetNegative(ctx context.Context, kind domain.ResultKind, nameKey string) (bool, error)
	SetNegative(ctx context.Context, kind domain.ResultKind, nameKey string) error
}

type IdentityBridge interface {
	ExternalIDs(ctx context.Context, kind domain.ResultKind, mbid string) (map[string]string, bool)
}

type NameKeyedCache[T any] interface {
	Get(ctx context.Context, nameKey string) (T, bool, error)
	Set(ctx context.Context, nameKey string, v T) error
	GetNegative(ctx context.Context, nameKey string) (bool, error)
	SetNegative(ctx context.Context, nameKey string) error
}

type LastFmEnricher interface {
	Lookup(ctx context.Context, kind domain.ResultKind, artistName, entityTitle string) (domain.LastFmEnrichment, error)
}

type LastFmEnrichmentCache = NameKeyedCache[domain.LastFmEnrichment]

type DeezerEnricher interface {
	ResolveID(ctx context.Context, kind domain.ResultKind, artist, title string) (string, error)
	Lookup(ctx context.Context, kind domain.ResultKind, id string) (domain.DeezerEnrichment, error)
}

type DeezerEnrichmentCache = NameKeyedCache[domain.DeezerEnrichment]

type LyricsProvider interface {
	ResolveTrackID(ctx context.Context, artist, title string) (string, error)
	Lookup(ctx context.Context, trackID string) (domain.DeezerLyrics, error)
}

type LyricsCache = NameKeyedCache[domain.DeezerLyrics]
