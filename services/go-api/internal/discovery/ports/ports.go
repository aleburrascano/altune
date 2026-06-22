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

// EventStore appends telemetry interaction events (§8 of the discovery rebuild
// blueprint). Append is best-effort from the caller's perspective — emission is
// async and a failure must never surface to the user request.
type EventStore interface {
	Append(ctx context.Context, event domain.InteractionEvent) error
}

// QueryCount is a query_norm with how many times it occurred in a window.
type QueryCount struct {
	QueryNorm string
	Count     int
}

// EventQuery reads aggregated telemetry for the offline coverage signals. These
// are analytics reads over discovery's own tables — never the request path.
type EventQuery interface {
	// ZeroResultQueries ranks search queries that returned nothing in the window.
	ZeroResultQueries(ctx context.Context, since time.Time, limit int) ([]QueryCount, error)
	// NonZeroNoClickQueries ranks queries that returned results but drew no click
	// for that query_norm in the window (a weak coverage hint).
	NonZeroNoClickQueries(ctx context.Context, since time.Time, limit int) ([]QueryCount, error)
}

type AlbumContentProvider interface {
	GetAlbumTracks(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)
}

type ArtistContentProvider interface {
	GetArtistTopTracks(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)
	GetArtistAlbums(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)
}

// RelatedTracksProvider returns a provider's per-track "related" recommendation
// set, keyed by the track's external id. Track-keyed sibling of
// ArtistContentProvider; only SoundCloud implements it today
// (/tracks/{id}/related).
type RelatedTracksProvider interface {
	GetRelatedTracks(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)
}

// MetadataEnricher resolves and looks up MusicBrainz enrichment for a single
// entity. ResolveMBID maps a (kind, title, subtitle) to an MBID via strict
// name match (""+nil when none). Lookup fetches the inc= enrichment for a known
// MBID. Implemented by the MusicBrainz adapter; consumed by EnrichmentService.
type MetadataEnricher interface {
	ResolveMBID(ctx context.Context, kind domain.ResultKind, title, subtitle string) (string, error)
	Lookup(ctx context.Context, kind domain.ResultKind, mbid string) (domain.MBEnrichment, error)
}

// EnrichmentCache is a read-through cache of MBEnrichment. Positive entries key
// by (kind, mbid) and store the whole value object (artwork included). The
// negative path keys by (kind, nameKey) so an unresolved entity is not
// re-resolved every open. A nil-backed implementation is a no-op.
type EnrichmentCache interface {
	Get(ctx context.Context, kind domain.ResultKind, mbid string) (domain.MBEnrichment, bool, error)
	Set(ctx context.Context, kind domain.ResultKind, mbid string, e domain.MBEnrichment) error
	GetNegative(ctx context.Context, kind domain.ResultKind, nameKey string) (bool, error)
	SetNegative(ctx context.Context, kind domain.ResultKind, nameKey string) error
}

// IdentityBridge maps a resolved MusicBrainz entity to its cross-provider ids
// (the bare ids MB asserts via url-relations: deezer/spotify/discogs/...). It is
// the read side of the MB enrichment cache: a hit means some prior detail-open
// enriched this MBID and cached its external_ids. Consumed by the merge step to
// resolve identity across providers; a nil-backed implementation returns no ids,
// so merge silently falls back to name similarity.
type IdentityBridge interface {
	ExternalIDs(ctx context.Context, kind domain.ResultKind, mbid string) (map[string]string, bool)
}

// MBIDIndex is a cache-only name→MBID memo. A detail-open's strict name
// resolution remembers (kind, nameKey) → mbid; the search path reads it to
// attach an MBID to a non-MB result so the MBID-keyed artwork tier (Cover Art
// Archive / Fanart.tv) fires on the search card too. Cache-only — never an MB
// call on the search path; a miss degrades to the provider's own thumbnail.
type MBIDIndex interface {
	LookupMBID(ctx context.Context, kind domain.ResultKind, nameKey string) (string, bool)
	RememberMBID(ctx context.Context, kind domain.ResultKind, nameKey, mbid string) error
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
	Area           string
	ArtistType     string
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

// DiscogsEnricher resolves and looks up Discogs album enrichment for the
// detail-open surface (docs/providers/discogs.md caps 3–6). ResolveMasterID maps
// (artist, album) to a Discogs master id via the structured artist+title search
// (0 when none). LookupAlbum fetches the master + its main release and assembles
// the credits/styles/label/community enrichment. Implemented by the Discogs
// adapter; consumed by DiscogsEnrichmentService.
type DiscogsEnricher interface {
	ResolveMasterID(ctx context.Context, artist, album string) (int, error)
	LookupAlbum(ctx context.Context, masterID int) (domain.DiscogsEnrichment, error)
	// ResolveArtistID maps an artist name to a Discogs artist id (0 when none);
	// LookupArtist fetches the bio/aliases/groups/links for a known id (cap 7).
	ResolveArtistID(ctx context.Context, name string) (int, error)
	LookupArtist(ctx context.Context, artistID int) (domain.DiscogsArtistEnrichment, error)
}

// DiscogsEnrichmentCache is a read-through cache of DiscogsEnrichment keyed by a
// normalized (artist, album) name key — Discogs has no ISRC/MBID, so the name
// key is the only stable handle on the request. The negative path records that a
// name resolved to nothing, so an unresolved album is not re-resolved every open.
// A nil-backed implementation is a no-op.
type DiscogsEnrichmentCache interface {
	Get(ctx context.Context, nameKey string) (domain.DiscogsEnrichment, bool, error)
	Set(ctx context.Context, nameKey string, e domain.DiscogsEnrichment) error
	GetNegative(ctx context.Context, nameKey string) (bool, error)
	SetNegative(ctx context.Context, nameKey string) error
}

// DiscogsArtistEnrichmentCache is the artist-scoped sibling of
// DiscogsEnrichmentCache, keyed by a normalized artist name. A nil-backed
// implementation is a no-op.
type DiscogsArtistEnrichmentCache interface {
	Get(ctx context.Context, nameKey string) (domain.DiscogsArtistEnrichment, bool, error)
	Set(ctx context.Context, nameKey string, e domain.DiscogsArtistEnrichment) error
	GetNegative(ctx context.Context, nameKey string) (bool, error)
	SetNegative(ctx context.Context, nameKey string) error
}

// LastFmEnricher looks up Last.fm detail-open enrichment for one entity
// (docs/providers/lastfm.md cap 3). Last.fm's *.getInfo methods take entity
// names directly and fuzzy-match server-side (autocorrect), so there is no
// separate id-resolution step — a single Lookup per opened entity. artistName
// is the artist; entityTitle is the track/album title (empty for the artist
// kind). Implemented by the Last.fm adapter; consumed by LastFmEnrichmentService.
type LastFmEnricher interface {
	Lookup(ctx context.Context, kind domain.ResultKind, artistName, entityTitle string) (domain.LastFmEnrichment, error)
}

// LastFmEnrichmentCache is a read-through cache of LastFmEnrichment keyed by a
// normalized (kind, artist, title) name key — Last.fm has no stable id for the
// request, so the name key is the handle. The negative path records that a name
// resolved to nothing, so an unresolved entity is not re-looked-up every open.
// A nil-backed implementation is a no-op.
type LastFmEnrichmentCache interface {
	Get(ctx context.Context, nameKey string) (domain.LastFmEnrichment, bool, error)
	Set(ctx context.Context, nameKey string, e domain.LastFmEnrichment) error
	GetNegative(ctx context.Context, nameKey string) (bool, error)
	SetNegative(ctx context.Context, nameKey string) error
}

// DeezerEnricher resolves and looks up Deezer detail-open enrichment for one
// entity (docs/providers/deezer.md caps 7–8). ResolveID maps a (kind, artist,
// title) to a Deezer track/album id via search ("" when none). Lookup fetches
// the /track/{id} or /album/{id} detail and assembles the audio fields / album
// liner data. Implemented by the Deezer adapter; consumed by
// DeezerEnrichmentService. Lyrics (cap 6) are a separate feature, not here.
type DeezerEnricher interface {
	ResolveID(ctx context.Context, kind domain.ResultKind, artist, title string) (string, error)
	Lookup(ctx context.Context, kind domain.ResultKind, id string) (domain.DeezerEnrichment, error)
}

// DeezerEnrichmentCache is a read-through cache of DeezerEnrichment keyed by a
// normalized (kind, artist, title) name key — the request handle, since the
// detail endpoint is reached by name-resolve. The negative path records that a
// name resolved to nothing, so an unresolved entity is not re-resolved every
// open. A nil-backed implementation is a no-op.
type DeezerEnrichmentCache interface {
	Get(ctx context.Context, nameKey string) (domain.DeezerEnrichment, bool, error)
	Set(ctx context.Context, nameKey string, e domain.DeezerEnrichment) error
	GetNegative(ctx context.Context, nameKey string) (bool, error)
	SetNegative(ctx context.Context, nameKey string) error
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
	FindRelatedByAlbum(ctx context.Context, album string, limit int) ([]RelatedTrackMatch, error)
	FindRelatedByArtist(ctx context.Context, artist string, limit int) ([]RelatedTrackMatch, error)
}
