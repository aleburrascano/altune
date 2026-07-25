package domain

import (
	"fmt"
	"time"

	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/textnorm"

	"github.com/google/uuid"
)

type ResultKind int

const (
	ResultKindUnknown ResultKind = iota
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

type EntityResolutionTier int

const (
	EntityResolutionNone EntityResolutionTier = iota
	EntityResolutionISRC
	EntityResolutionUPC
	EntityResolutionMBID
	EntityResolutionBridge
)

func (t EntityResolutionTier) String() string {
	switch t {
	case EntityResolutionMBID:
		return "mbid"
	case EntityResolutionISRC:
		return "isrc"
	case EntityResolutionUPC:
		return "upc"
	case EntityResolutionBridge:
		return "bridge"
	case EntityResolutionNone:
		return "none"
	default:
		return "unknown"
	}
}

func ResolutionTierFromExtras(extras map[string]any) EntityResolutionTier {
	s, _ := extras["resolution_tier"].(string)
	switch s {
	case "mbid":
		return EntityResolutionMBID
	case "isrc":
		return EntityResolutionISRC
	case "upc":
		return EntityResolutionUPC
	case "bridge":
		return EntityResolutionBridge
	default:
		return EntityResolutionNone
	}
}

type ProviderName int

const (
	ProviderUnknown ProviderName = iota
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
	ProviderSpotify
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
	case ProviderSpotify:
		return "spotify"
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
	case "spotify":
		return ProviderSpotify, nil
	default:
		return 0, fmt.Errorf("unknown provider: %s", s)
	}
}

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

type SourceRef struct {
	Provider   ProviderName
	ExternalID string
	URL        string
}

type SearchResult struct {
	Kind          ResultKind
	Title         string
	Subtitle      string
	ImageURL      string
	ArtworkSource string
	Confidence    Confidence
	Sources       []SourceRef
	Popularity    float64
	ISRC          string
	MBID          string
	UPC           string
	Xref          map[string]string
	Year          int
	ReleaseDate   string
	TrackCount    int
	ProviderRank  int64
	FanCount      int64
	Album         string
	Duration      int
	DeezerAlbumID string
	Signature     string
	Extras        map[string]any
}

type CollapsedArtistSummary struct {
	Title    string         `json:"title"`
	Subtitle string         `json:"subtitle"`
	ImageURL string         `json:"image_url,omitempty"`
	Sources  []SourceRef    `json:"sources"`
	Extras   map[string]any `json:"extras"`
}

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

func ResultSignature(r SearchResult) string {
	return r.Kind.String() + "|" +
		textnorm.NormalizeForMatch(r.Title) + "|" +
		textnorm.NormalizeForMatch(r.Subtitle)
}

type SearchQuery struct {
	Raw    string
	Kinds  map[ResultKind]bool
	Limit  int
	Offset int
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

const MaxSearchOffset = 200

func NewPagedSearchQuery(raw string, kinds map[ResultKind]bool, limit, offset int) (*SearchQuery, error) {
	q, err := NewSearchQuery(raw, kinds, limit)
	if err != nil {
		return nil, err
	}
	if offset < 0 || offset > MaxSearchOffset {
		return nil, fmt.Errorf("offset must be between 0 and %d", MaxSearchOffset)
	}
	q.Offset = offset
	return q, nil
}

type SearchHistoryEntry struct {
	ID                     uuid.UUID
	UserId                 shared.UserId
	Query                  string
	QueryNorm              string
	ExecutedAt             time.Time
	ResultClickedSignature *string
}

type ProviderSearchResponse struct {
	Provider    ProviderName
	Results     []SearchResult
	Status      ProviderStatus
	LatencyMs   int64
	ResultCount int
}

type RelatedGroup struct {
	Relationship string
	RelatedTo    string
	Items        []SearchResult
}
