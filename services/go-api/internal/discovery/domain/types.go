package domain

import (
	"fmt"
	"time"

	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/textnorm"

	"github.com/google/uuid"
)

// ResultKind discriminates what kind of music a SearchResult represents.
type ResultKind int

const (
	ResultKindUnknown  ResultKind = iota
	ResultKindArtist
	ResultKindAlbum
	ResultKindTrack
	ResultKindPlaylist
)

func (k ResultKind) String() string {
	switch k {
	case ResultKindUnknown:
		return "unknown"
	case ResultKindArtist:
		return "artist"
	case ResultKindAlbum:
		return "album"
	case ResultKindTrack:
		return "track"
	case ResultKindPlaylist:
		return "playlist"
	default:
		return "unknown"
	}
}

func ParseResultKind(s string) (ResultKind, error) {
	switch s {
	case "artist":
		return ResultKindArtist, nil
	case "album":
		return ResultKindAlbum, nil
	case "track":
		return ResultKindTrack, nil
	case "playlist":
		return ResultKindPlaylist, nil
	default:
		return 0, fmt.Errorf("unknown result kind: %s", s)
	}
}

// Confidence indicates dedup merge confidence level.
type Confidence int

const (
	ConfidenceLow Confidence = iota
	ConfidenceMedium
	ConfidenceHigh
)

func ParseConfidence(s string) (Confidence, error) {
	switch s {
	case "high":
		return ConfidenceHigh, nil
	case "medium":
		return ConfidenceMedium, nil
	case "low":
		return ConfidenceLow, nil
	default:
		return 0, fmt.Errorf("unknown confidence: %s", s)
	}
}

func (c Confidence) String() string {
	switch c {
	case ConfidenceHigh:
		return "high"
	case ConfidenceMedium:
		return "medium"
	case ConfidenceLow:
		return "low"
	default:
		return "unknown"
	}
}

// EntityResolutionTier indicates how two results were identified as the same entity.
type EntityResolutionTier int

const (
	EntityResolutionNone EntityResolutionTier = iota
	EntityResolutionISRC
	EntityResolutionMBID
	// EntityResolutionBridge: two results were identified as the same entity via
	// the MusicBrainz cross-provider id bridge (an MB entity's url-relation
	// asserts its Deezer/Spotify/Discogs id, which matches another provider's
	// native id). Identity-grade, like ISRC/MBID.
	EntityResolutionBridge
)

func (t EntityResolutionTier) String() string {
	switch t {
	case EntityResolutionMBID:
		return "mbid"
	case EntityResolutionISRC:
		return "isrc"
	case EntityResolutionBridge:
		return "bridge"
	case EntityResolutionNone:
		return "none"
	default:
		return "unknown"
	}
}

// ResolutionTierFromExtras reads the "resolution_tier" wire key merge.go stamps
// on every merged result back into the typed tier. Returns EntityResolutionNone
// when the key is absent or not a recognized tier string, so a reader outside
// the writer's file gets a compile-checked accessor instead of a raw string
// comparison against SearchResult.Extras.
func ResolutionTierFromExtras(extras map[string]any) EntityResolutionTier {
	s, _ := extras["resolution_tier"].(string)
	switch s {
	case "mbid":
		return EntityResolutionMBID
	case "isrc":
		return EntityResolutionISRC
	case "bridge":
		return EntityResolutionBridge
	default:
		return EntityResolutionNone
	}
}

// ProviderName identifies a music data provider.
type ProviderName int

const (
	ProviderUnknown    ProviderName = iota
	ProviderDeezer
	ProviderMusicBrainz
	ProviderSoundCloud
	ProviderLastFM
	ProviderITunes
	ProviderTheAudioDB
	ProviderDiscogs
	ProviderYouTube
	ProviderAmazonMusic
	ProviderAppleMusic
)

func (p ProviderName) String() string {
	switch p {
	case ProviderUnknown:
		return "unknown"
	case ProviderDeezer:
		return "deezer"
	case ProviderMusicBrainz:
		return "musicbrainz"
	case ProviderSoundCloud:
		return "soundcloud"
	case ProviderLastFM:
		return "lastfm"
	case ProviderITunes:
		return "itunes"
	case ProviderTheAudioDB:
		return "theaudiodb"
	case ProviderDiscogs:
		return "discogs"
	case ProviderYouTube:
		return "youtube"
	case ProviderAmazonMusic:
		return "amazonmusic"
	case ProviderAppleMusic:
		return "applemusic"
	default:
		return "unknown"
	}
}

func ParseProviderName(s string) (ProviderName, error) {
	switch s {
	case "deezer":
		return ProviderDeezer, nil
	case "musicbrainz":
		return ProviderMusicBrainz, nil
	case "soundcloud":
		return ProviderSoundCloud, nil
	case "lastfm":
		return ProviderLastFM, nil
	case "itunes":
		return ProviderITunes, nil
	case "theaudiodb":
		return ProviderTheAudioDB, nil
	case "discogs":
		return ProviderDiscogs, nil
	case "youtube":
		return ProviderYouTube, nil
	case "amazonmusic":
		return ProviderAmazonMusic, nil
	case "applemusic":
		return ProviderAppleMusic, nil
	default:
		return 0, fmt.Errorf("unknown provider: %s", s)
	}
}

// ProviderStatus is the outcome of a scatter-gather call to one provider.
type ProviderStatus int

const (
	ProviderStatusOK ProviderStatus = iota
	ProviderStatusTimeout
	ProviderStatusError
	ProviderStatusRateLimited
	ProviderStatusCircuitOpen
)

func (s ProviderStatus) String() string {
	switch s {
	case ProviderStatusOK:
		return "ok"
	case ProviderStatusTimeout:
		return "timeout"
	case ProviderStatusError:
		return "error"
	case ProviderStatusRateLimited:
		return "rate_limited"
	case ProviderStatusCircuitOpen:
		return "circuit_open"
	default:
		return "unknown"
	}
}

// SourceRef is one provider's reference to a merged SearchResult.
type SourceRef struct {
	Provider   ProviderName
	ExternalID string
	URL        string
}

// SearchResult is the merged + deduped discovery result.
type SearchResult struct {
	Kind       ResultKind
	Title      string
	Subtitle   string
	ImageURL   string
	// ArtworkSource records WHERE ImageURL came from, for per-provider coverage
	// visibility: a resolver source ("fanart", "discogs", "itunes", …) when the
	// artwork chain resolved it, the search provider's own name when the result's
	// provider image was kept, "cached" on an artwork-cache hit, or "" when no
	// artwork was found. Display/telemetry only — never a ranking or merge input.
	ArtworkSource string
	Confidence    Confidence
	Sources       []SourceRef
	// Popularity is the continuous relevance-tiebreak Rank reads after relevance.
	// It is a TYPED field (not an Extras key) so the producer→consumer link is
	// compile-checked rather than a silently-absent map entry — the gap the
	// strangler collapse opened when it deleted popularity.go but kept Rank's tier.
	//
	// AIDEV-WARNING: intentionally UNPOPULATED by providers today. A naive revival
	// (Deezer track→rank, artist/album→nb_fan) was eval-rejected 2026-06-22: it
	// regressed the top-3 gate on "Scorpion" because albums report nb_fan=0, so a
	// high-rank obscure track buries the canonical album. A fair revival needs
	// per-kind-comparable popularity (e.g. album positional fallback / per-kind
	// normalization) and must clear `discoveryeval -mode eval -top-k 3` plus the
	// canonical ambiguous-query set before it is wired. The machinery (merge max,
	// Rank tier) is live and unit-tested; only the producer is deliberately absent.
	Popularity float64
	// Identity keys, typed for the same reason as Popularity: merge's identity
	// tiers branch on them, so the producer→consumer link must be compile-checked
	// rather than a silently-absent (or type-drifted) map entry. ""/nil = absent.
	ISRC string
	MBID string
	// Xref carries the bridged cross-provider ids (provider name → external id)
	// stamped pre-merge from the identity bridge (MusicBrainz url-relations) or
	// recovered from the durable identity store. Merge and artwork resolution
	// read it as identity proof.
	Xref map[string]string
	// Release metadata read by consensus clustering and discography ordering.
	// 0/"" = unknown. Year is derived from ReleaseDate when a provider carries
	// only the date (see normalizeAlbumYears).
	Year        int
	ReleaseDate string
	TrackCount  int
	// Provider prominence raw signals read by the cross-kind prominence
	// tiebreak: Deezer's track rank and artist/album fan count. 0 = none.
	ProviderRank int64
	FanCount     int64
	// Album and Duration are typed for the same reason as Popularity/ISRC/MBID:
	// completenessOf (merge) and isLowConfidenceTail (rank) branch on them, so
	// the producer→consumer link must be compile-checked rather than a
	// silently-absent/mistyped Extras entry. ""/0 = unknown.
	Album    string
	Duration int
	// DeezerAlbumID is typed for the same reason: FindRelatedService branches on
	// it to decide whether to look up Deezer album tracks, so the producer
	// (deezer.go)→consumer (find_related.go) link must be compile-checked
	// rather than a silently-absent/mistyped Extras entry. "" = unknown.
	DeezerAlbumID string
	// Extras carries provider-specific DISPLAY/TELEMETRY metadata only — nothing
	// merge, rank, or consensus branches on (those keys are typed fields above).
	// Current keys: "album", "duration" (mirrored from the typed fields above for
	// wire compatibility), "preview_url", "record_type", "genre",
	// "genre_id", "disambiguation", "artist_type", "area", "mb_tags",
	// "playcount", "playback_count", "likes_count", "reposts_count",
	// "track_number", "disc_number", "explicit", "copyright",
	// "featured_artists" ([]map[string]any), "collapsed_artists"
	// ([]CollapsedArtistSummary), "resolution_tier", "consensus_*", "artwork_path".
	Extras map[string]any
}

// CollapsedArtistSummary is the typed shape stored in the "collapsed_artists"
// extra by CollapseArtistDuplicates: same-name artists folded into the primary.
// JSON tags match the pre-existing wire keys so clients are unaffected.
type CollapsedArtistSummary struct {
	Title    string         `json:"title"`
	Subtitle string         `json:"subtitle"`
	ImageURL string         `json:"image_url,omitempty"`
	Sources  []SourceRef    `json:"sources"`
	Extras   map[string]any `json:"extras"`
}

// NewProviderResult builds the standard single-source, low-confidence result a
// provider emits from one of its catalog entries. It is the single home for that
// shape: the ConfidenceLow default, the one-element Sources wrapping, and the
// nil-safe Extras initialization (providers that don't carry extras pass nil and
// still get a writable map — the nil-map footgun the wire mapper used to guard).
func NewProviderResult(kind ResultKind, title, subtitle, imageURL string, source SourceRef, extras map[string]any) SearchResult {
	if extras == nil {
		extras = map[string]any{}
	}
	return SearchResult{
		Kind:       kind,
		Title:      title,
		Subtitle:   subtitle,
		ImageURL:   imageURL,
		Confidence: ConfidenceLow,
		Sources:    []SourceRef{source},
		Extras:     extras,
	}
}

// ResultSignature is the server-computed stable identity of a result —
// (kind, normalized title, normalized subtitle). It is the cross-query,
// cross-provider join key for the engagement funnel: the handler emits it on
// the wire, the client echoes it on every engagement event, and behavioral
// ranking keys its score map by it. Single definition on purpose — the rank
// pipeline and the wire mapper MUST compute the same bytes or behavioral
// scores silently stop joining.
func ResultSignature(r SearchResult) string {
	return r.Kind.String() + "|" +
		textnorm.NormalizeForMatch(r.Title) + "|" +
		textnorm.NormalizeForMatch(r.Subtitle)
}

// SearchQuery is the validated user search input.
type SearchQuery struct {
	Raw   string
	Kinds map[ResultKind]bool
	Limit int
}

func NewSearchQuery(raw string, kinds map[ResultKind]bool, limit int) (*SearchQuery, error) {
	if raw == "" {
		return nil, fmt.Errorf("raw query cannot be empty")
	}
	if len(kinds) == 0 {
		return nil, fmt.Errorf("kinds cannot be empty")
	}
	if limit < 1 || limit > 50 {
		return nil, fmt.Errorf("limit must be between 1 and 50")
	}
	return &SearchQuery{
		Raw:   raw,
		Kinds: kinds,
		Limit: limit,
	}, nil
}

// SearchHistoryEntry is a persisted search-history row.
type SearchHistoryEntry struct {
	ID                     uuid.UUID
	UserId                 shared.UserId
	Query                  string
	QueryNorm              string
	ExecutedAt             time.Time
	ResultClickedSignature *string
}

// ProviderSearchResponse wraps a provider's results with metadata.
type ProviderSearchResponse struct {
	Provider    ProviderName
	Results     []SearchResult
	Status      ProviderStatus
	LatencyMs   int64
	ResultCount int
}

// RelatedGroup is a set of results related to an organic search result.
type RelatedGroup struct {
	Relationship string         // "album_tracks", "artist_albums", "library_matches"
	RelatedTo    string         // title of the organic result that triggered this group
	Items        []SearchResult
}
