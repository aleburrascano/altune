package ports

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/ids"
	"context"
	"fmt"
	"time"
)

func IsUnverifiedArtworkMiss(url string, err error) bool {
	return url == "" && err != nil
}

type ArtworkLookup interface {
	Resolve(ctx context.Context, kind domain.ResultKind, title, subtitle string, mbid string) (string, error)
	ArtworkSource() domain.ProviderKey
}

type TaggingArtworkLookup interface {
	ResolveTagged(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (url string, source domain.ProviderKey, err error)
	ResolveWithIdentityTagged(ctx context.Context, kind domain.ResultKind, title, subtitle string, id ArtworkIdentity) (url string, source domain.ProviderKey, err error)
}

type ArtworkIdentity struct {
	MBID        string
	ExternalIDs map[domain.ProviderKey]string
}

func (id ArtworkIdentity) ExternalID(key domain.ProviderKey) string {
	return id.ExternalIDs[key]
}

func (id ArtworkIdentity) HasLinks() bool {
	return id.MBID != "" || len(id.ExternalIDs) > 0
}

type IdentityArtworkLookup interface {
	ResolveByIdentity(ctx context.Context, kind domain.ResultKind, id ArtworkIdentity) (string, error)
	ArtworkSource() domain.ProviderKey
}

type ArtworkConfidence int

const (
	ArtworkConfidenceNone ArtworkConfidence = iota
	ArtworkConfidenceName
	ArtworkConfidenceIdentity
)

type ArtworkCache interface {
	Get(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (url string, source domain.ProviderKey, found bool, err error)
	Set(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid, url string, source domain.ProviderKey, confidence ArtworkConfidence) error
}

type ArtistIdentityResolver interface {
	ResolveArtistIdentity(ctx context.Context, artistName string) (*ArtistIdentity, error)
}

type ArtistIdentity struct {
	MBID           string
	Disambiguation string
	BirthYear      int
	Area           string
	ArtistType     string
}

type RelatedTrackMatch struct {
	Title      string
	Artist     string
	Album      string
	ArtworkURL *string
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

type RelationshipQuerier interface {
	FindRelatedByAlbumArtist(ctx context.Context, userId ids.UserId, album, artist string, limit int) ([]RelatedTrackMatch, error)
}

type DiscographyCase struct {
	ArtistRef          string
	Releases           int
	SingleProvider     int
	SingleProviderNoID int
	ProviderCounts     map[string]int
	LastSeen           time.Time
}

type DiscographyGroupBy string

const (
	GroupByArtist            DiscographyGroupBy = "artist"
	GroupByProvider          DiscographyGroupBy = "provider"
	GroupByContaminationBand DiscographyGroupBy = "contamination_band"
)

func ParseDiscographyGroupBy(raw string) (DiscographyGroupBy, error) {
	switch groupBy := DiscographyGroupBy(raw); groupBy {
	case GroupByArtist, GroupByProvider, GroupByContaminationBand:
		return groupBy, nil
	default:
		return "", fmt.Errorf("%w: %q", domain.ErrUnknownDiscographyGroupBy, raw)
	}
}

type DiscographySuspectRate struct {
	Rate       float64
	LastSample time.Time
}

type DiscographyQualityReader interface {
	DiscographyQuality(ctx context.Context, since time.Time, groupBy DiscographyGroupBy, limit int) ([]DiscographyCase, error)
	SuspectRate(ctx context.Context, since time.Time) (DiscographySuspectRate, error)
}

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

type EventStore interface {
	Append(ctx context.Context, event domain.InteractionEvent) error
}

type ActivityFeed interface {
	EmitActivity(eventType string)
}

type QueryCount struct {
	QueryNorm string
	Count     int
}

type BehavioralSignal struct {
	ResultSignature string
	Score           float64
}

type ListeningSignalStore interface {
	SatisfactionSignals(ctx context.Context, since time.Time) ([]BehavioralSignal, error)
}

type EventQuery interface {
	NoResultSearches(ctx context.Context, since time.Time, limit int) ([]QueryCount, error)
	NonZeroNoClickQueries(ctx context.Context, since time.Time, limit int) ([]QueryCount, error)
	AbandonedSearches(ctx context.Context, since time.Time, limit int) ([]QueryCount, error)
}

const MaxFavoritesPerUser = 1000

type FavoritesRepository interface {
	Add(ctx context.Context, userId ids.UserId, fav domain.Favorite) error
	Remove(ctx context.Context, userId ids.UserId, kind domain.ResultKind, key string) error
	ListForUser(ctx context.Context, userId ids.UserId) ([]domain.Favorite, error)
}

type HistoryWriter interface {
	Insert(ctx context.Context, entry *domain.SearchHistoryEntry) error
	TrimToN(ctx context.Context, userId ids.UserId, n int) error
}

type HistoryReader interface {
	ListDistinctRecent(ctx context.Context, userId ids.UserId, limit int) ([]*domain.SearchHistoryEntry, error)
}

type HistoryEraser interface {
	EraseSearchTextForUser(ctx context.Context, userId ids.UserId) error
}

type DeletedUserDataEraser interface {
	EraseRowsOfDeletedIdentities(ctx context.Context) (int64, error)
}

type EventRetention interface {
	PruneDiscographyObserved(ctx context.Context, now time.Time) (int64, error)
	PruneEvents(ctx context.Context, now time.Time) (int64, error)
}

type MBIDIndex interface {
	LookupMBID(ctx context.Context, kind domain.ResultKind, nameKey string) (string, bool)
	RememberMBID(ctx context.Context, kind domain.ResultKind, nameKey, mbid string) error
}

type IdentityStore interface {
	LookupByProviderID(ctx context.Context, kind domain.ResultKind, provider domain.ProviderKey, externalID string) (mbid string, xref map[domain.ProviderKey]string, ok bool)
	BatchIdentityLookup
}

type IdentityRef struct {
	Kind       domain.ResultKind
	Provider   domain.ProviderKey
	ExternalID string
}

type IdentityHit struct {
	MBID string
	Xref map[domain.ProviderKey]string
}

type BatchIdentityLookup interface {
	LookupByProviderIDs(ctx context.Context, refs []IdentityRef) map[IdentityRef]IdentityHit
}

type OwnedTrack struct {
	TrackID           string
	Title             string
	Artist            string
	AcquisitionStatus string
	TrackNumber       *int
	ISRC              string
	Album             string
}

type TrackNumberFiller interface {
	FillTrackNumber(ctx context.Context, userId ids.UserId, trackId ids.TrackId, trackNumber int) error
}

type OwnershipReader interface {
	OwnedByTitleArtist(ctx context.Context, userId ids.UserId) (map[string]OwnedTrack, error)
}

type OwnedTrackLister interface {
	ListOwnedTracks(ctx context.Context, userId ids.UserId) ([]OwnedTrack, error)
}

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

type GeniusSongCatalog interface {
	FindArtistID(ctx context.Context, artistName string, knownTitles []string) (int, bool, error)
	ArtistSongs(ctx context.Context, artistID, maxPages int) ([]domain.UnreleasedSong, error)
	SongDetail(ctx context.Context, songID int) (domain.GeniusSongDetail, error)
}

type ArtistCatalogTitles interface {
	CatalogTitles(ctx context.Context, provider domain.ProviderName, externalID string, known map[domain.ProviderName]string) ([]string, error)
}

type UnreleasedCache interface {
	Get(ctx context.Context, provider domain.ProviderName, externalID, nameKey string) ([]domain.UnreleasedSong, bool)
	Set(ctx context.Context, provider domain.ProviderName, externalID, nameKey string, songs []domain.UnreleasedSong, ttl time.Duration) error
}

type UnreleasedArtistKey struct {
	Provider   domain.ProviderName
	ExternalID string
}

type UnreleasedDueArtist struct {
	Key      UnreleasedArtistKey
	Name     string
	KnownIDs map[domain.ProviderName]string
}

type UnreleasedHit struct {
	Artist     UnreleasedArtistKey
	ArtistName string
	Song       domain.UnreleasedSong
}

type UnreleasedStore interface {
	Record(ctx context.Context, key UnreleasedArtistKey, name string, knownIDs map[domain.ProviderName]string) error
	Read(ctx context.Context, key UnreleasedArtistKey) (songs []domain.UnreleasedSong, complete, built bool, buildVersion int, err error)
	Write(ctx context.Context, key UnreleasedArtistKey, songs []domain.UnreleasedSong, complete bool, startedAt time.Time, buildVersion int) error
	Due(ctx context.Context, now time.Time, limit int) ([]UnreleasedDueArtist, error)
	Search(ctx context.Context, query string, limit int) ([]UnreleasedHit, error)
}

type TrackerSongs interface {
	ArtistSongs(ctx context.Context, artistName string) ([]domain.UnreleasedSong, error)
}

type TrackerIndexCache interface {
	Get(ctx context.Context) (map[string]string, bool)
	Set(ctx context.Context, index map[string]string, ttl time.Duration) error
}

type SpellingHintsReader interface {
	SuggestByPrefix(ctx context.Context, prefix string, limit int) ([]domain.SpellingHintsEntry, error)
	FindClosest(ctx context.Context, query string, limit int) ([]domain.SpellingHintsEntry, error)
}

type SpellingHintsWriter interface {
	Add(ctx context.Context, entry domain.SpellingHintsEntry) error
	BulkAdd(ctx context.Context, entries []domain.SpellingHintsEntry) error
	Trim(ctx context.Context, maxEntries int) error
}

type SpellingHintsStore interface {
	SpellingHintsReader
	SpellingHintsWriter
}

type ChartProvider interface {
	Name() domain.ProviderName
	FetchCharts(ctx context.Context, limit int) ([]domain.SpellingHintsEntry, error)
}
