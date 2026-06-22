package ports

import (
	"context"

	"altune/go-api/internal/discovery/domain"
)

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

// NameKeyedCache is the read-through detail-enrichment cache shape shared by the
// name-resolved enrichers (Deezer, Last.fm, Discogs album/artist, lyrics): a
// positive value keyed by a normalized name key, plus a negative marker that the
// name produced nothing, so it is not re-resolved every open. The MB
// EnrichmentCache is intentionally NOT this shape (it keys positives by mbid). A
// nil-backed implementation is a no-op. CachedLookup (service layer) drives it.
type NameKeyedCache[T any] interface {
	Get(ctx context.Context, nameKey string) (T, bool, error)
	Set(ctx context.Context, nameKey string, v T) error
	GetNegative(ctx context.Context, nameKey string) (bool, error)
	SetNegative(ctx context.Context, nameKey string) error
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

// DiscogsEnrichmentCache caches DiscogsEnrichment keyed by a normalized
// (artist, album) name key — Discogs has no ISRC/MBID, so the name key is the
// only stable handle on the request.
type DiscogsEnrichmentCache = NameKeyedCache[domain.DiscogsEnrichment]

// DiscogsArtistEnrichmentCache is the artist-scoped sibling of
// DiscogsEnrichmentCache, keyed by a normalized artist name.
type DiscogsArtistEnrichmentCache = NameKeyedCache[domain.DiscogsArtistEnrichment]

// LastFmEnricher looks up Last.fm detail-open enrichment for one entity
// (docs/providers/lastfm.md cap 3). Last.fm's *.getInfo methods take entity
// names directly and fuzzy-match server-side (autocorrect), so there is no
// separate id-resolution step — a single Lookup per opened entity. artistName
// is the artist; entityTitle is the track/album title (empty for the artist
// kind). Implemented by the Last.fm adapter; consumed by LastFmEnrichmentService.
type LastFmEnricher interface {
	Lookup(ctx context.Context, kind domain.ResultKind, artistName, entityTitle string) (domain.LastFmEnrichment, error)
}

// LastFmEnrichmentCache caches LastFmEnrichment keyed by a normalized
// (kind, artist, title) name key — Last.fm has no stable id for the request, so
// the name key is the handle.
type LastFmEnrichmentCache = NameKeyedCache[domain.LastFmEnrichment]

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

// DeezerEnrichmentCache caches DeezerEnrichment keyed by a normalized
// (kind, artist, title) name key — the request handle, since the detail endpoint
// is reached by name-resolve.
type DeezerEnrichmentCache = NameKeyedCache[domain.DeezerEnrichment]

// LyricsProvider resolves and looks up Deezer lyrics for a single track
// (docs/providers/deezer.md cap 6). ResolveTrackID maps an (artist, title) to a
// Deezer track id via the public-API search ("" when none). Lookup fetches the
// time-synced + plain lyrics for a known track id via the internal pipe.deezer.com
// GraphQL (the anonymous-JWT path). A definitive "no lyrics for this track/region"
// returns an empty value + nil error (the service negative-caches it); a transient
// failure (auth/network) returns an error (not cached). Implemented by the Deezer
// lyrics adapter; consumed by LyricsService.
type LyricsProvider interface {
	ResolveTrackID(ctx context.Context, artist, title string) (string, error)
	Lookup(ctx context.Context, trackID string) (domain.DeezerLyrics, error)
}

// LyricsCache caches DeezerLyrics keyed by a normalized (artist, title) name key
// — the request handle, since lyrics are reached by name-resolve. Positive
// entries get a long TTL (lyrics are static); negative entries a short TTL
// (availability is region/catalog dependent) — set by the adapter constructor.
type LyricsCache = NameKeyedCache[domain.DeezerLyrics]

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
