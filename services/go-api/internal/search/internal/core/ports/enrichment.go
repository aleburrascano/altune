package ports

import (
	"altune/go-api/internal/search/domain"
	"context"
)

type DetailsProvider interface {
	ResolveMBID(ctx context.Context, kind domain.ResultKind, title, subtitle string) (string, error)
	Lookup(ctx context.Context, kind domain.ResultKind, mbid string) (domain.MusicBrainzEnrichment, error)
}

type EnrichmentCache interface {
	Get(ctx context.Context, kind domain.ResultKind, mbid string) (domain.MusicBrainzEnrichment, bool, error)
	Set(ctx context.Context, kind domain.ResultKind, mbid string, e domain.MusicBrainzEnrichment) error
	GetNegative(ctx context.Context, kind domain.ResultKind, nameKey string) (bool, error)
	SetNegative(ctx context.Context, kind domain.ResultKind, nameKey string) error
}

type NameKeyedCache[T any] interface {
	Get(ctx context.Context, nameKey string) (T, bool, error)
	Set(ctx context.Context, nameKey string, v T) error
	GetNegative(ctx context.Context, nameKey string) (bool, error)
	SetNegative(ctx context.Context, nameKey string) error
}

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

type ISRCRecording struct {
	MBID     string
	Duration int
}

type MusicBrainzRecording struct {
	MBID           string
	Title          string
	Disambiguation string
	Artists        []string
	ISRCs          []string
	Duration       float64
}

type MusicBrainzWorkRecording struct {
	MBID           string
	Title          string
	Disambiguation string
	ArtistIDs      []string
	Duration       float64
	Live           bool
}

type MusicBrainzRelease struct {
	Title  string
	Date   string
	Status string
}

type MusicBrainzRecordingDetail struct {
	MBID             string
	Title            string
	ArtistIDs        []string
	ISRCs            []string
	Duration         float64
	FirstReleaseDate string
	Releases         []MusicBrainzRelease
}
