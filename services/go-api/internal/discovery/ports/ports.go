package ports

import (
	"context"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"
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

type ArtworkResolver interface {
	Resolve(ctx context.Context, kind domain.ResultKind, title, subtitle string, mbid string) (string, error)
}

type ArtworkCache interface {
	Get(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (url string, found bool, err error)
	Set(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid, url string) error
}

type PopularityResolver interface {
	GetPopularity(ctx context.Context, title, artist string) (int64, error)
}

type QueryCache interface {
	Get(ctx context.Context, provider domain.ProviderName, kindsCSV, queryHash string) ([]domain.SearchResult, time.Time, bool, error)
	Set(ctx context.Context, provider domain.ProviderName, kindsCSV, queryHash string, results []domain.SearchResult) error
}

type SearchHistoryRepository interface {
	Insert(ctx context.Context, entry *domain.SearchHistoryEntry) error
	TrimToN(ctx context.Context, userId shared.UserId, n int) error
	ListDistinctRecent(ctx context.Context, userId shared.UserId, limit int) ([]*domain.SearchHistoryEntry, error)
}

type SearchClickRepository interface {
	InsertIfOutsideWindow(ctx context.Context, click *domain.SearchClick, windowSeconds int) (inserted bool, err error)
}

// ClickSignalProvider is an optional interface for retrieving click-based
// ranking signals. Implement when click volume justifies the DB query per search.
type ClickSignalProvider interface {
	TopClickedSignatures(ctx context.Context, queryNorm string, limit int) ([]string, error)
}

type AlbumContentProvider interface {
	GetAlbumTracks(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)
}

type ArtistContentProvider interface {
	GetArtistTopTracks(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)
	GetArtistAlbums(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)
}

type ContentValidationCache interface {
	Get(ctx context.Context, provider domain.ProviderName, externalID string) (domain.ContentValidationStatus, error)
	Set(ctx context.Context, provider domain.ProviderName, externalID string, status domain.ContentValidationStatus) error
}

type FetchSuccessStore interface {
	Record(ctx context.Context, provider domain.ProviderName, success bool) error
	GetRate(ctx context.Context, provider domain.ProviderName) (float64, error)
}

type MbidResolver interface {
	Resolve(ctx context.Context, url string) (string, error)
}

type VocabularyStore interface {
	Add(ctx context.Context, entry domain.VocabularyEntry) error
	BulkAdd(ctx context.Context, entries []domain.VocabularyEntry) error
	SuggestByPrefix(ctx context.Context, prefix string, limit int) ([]domain.VocabularyEntry, error)
	FindClosest(ctx context.Context, query string, limit int) ([]domain.VocabularyEntry, error)
}

type ChartProvider interface {
	FetchCharts(ctx context.Context, limit int) ([]domain.VocabularyEntry, error)
}

// AlbumValidator cross-references an artist's albums against an authoritative
// source (e.g., MusicBrainz) and splits them into confirmed vs unconfirmed.
type AlbumValidator interface {
	ValidateArtistAlbums(ctx context.Context, artistName string, albums []domain.SearchResult) (*AlbumValidationResult, error)
	ResolveArtistIdentity(ctx context.Context, artistName string) (*ArtistIdentity, error)
}

type ArtistIdentity struct {
	MBID           string
	Disambiguation string
	BirthYear      int
}

type AlbumValidationResult struct {
	Confirmed   []domain.SearchResult
	Unconfirmed []domain.SearchResult
	ArtistMBID  string
}

type RelatedTrackMatch struct {
	Title      string
	Artist     string
	Album      string
	ArtworkURL *string
}

type DiscographyEnricher interface {
	ResolveDiscogsArtist(ctx context.Context, name string, albumTitles []string) (*DiscogsArtistInfo, error)
	FetchArtistReleases(ctx context.Context, discogsID int) ([]DiscogsRelease, error)
}

type DiscogsArtistInfo struct {
	ID      int
	Name    string
	Genre   string
	Country string
	Overlap int
}

type DiscogsRelease struct {
	Title string
	Year  int
	Type  string
}

type AudioFingerprinter interface {
	VerifyArtist(ctx context.Context, audioData []byte) (mbid string, confidence float64, err error)
}

type RelationshipQuerier interface {
	FindRelatedByAlbum(ctx context.Context, album string, limit int) ([]RelatedTrackMatch, error)
	FindRelatedByArtist(ctx context.Context, artist string, limit int) ([]RelatedTrackMatch, error)
}
