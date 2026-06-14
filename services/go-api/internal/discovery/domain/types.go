package domain

import (
	"fmt"
	"time"

	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

// ResultKind discriminates what kind of music a SearchResult represents.
type ResultKind int

const (
	ResultKindArtist ResultKind = iota
	ResultKindAlbum
	ResultKindTrack
	ResultKindPlaylist
)

func (k ResultKind) String() string {
	switch k {
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
)

func (t EntityResolutionTier) String() string {
	switch t {
	case EntityResolutionMBID:
		return "mbid"
	case EntityResolutionISRC:
		return "isrc"
	case EntityResolutionNone:
		return "none"
	default:
		return "unknown"
	}
}

// ProviderName identifies a music data provider.
type ProviderName int

const (
	ProviderDeezer ProviderName = iota
	ProviderMusicBrainz
	ProviderSoundCloud
	ProviderLastFM
	ProviderITunes
	ProviderTheAudioDB
)

func (p ProviderName) String() string {
	switch p {
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
	default:
		return "unknown"
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

// ContentValidationStatus caches content fetch outcome.
type ContentValidationStatus int

const (
	ContentValidationUnknown ContentValidationStatus = iota
	ContentValidationFetchable
	ContentValidationUnfetchable
)

func (s ContentValidationStatus) String() string {
	switch s {
	case ContentValidationFetchable:
		return "fetchable"
	case ContentValidationUnfetchable:
		return "unfetchable"
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

// QualityScore is a composite quality signal.
type QualityScore struct {
	Completeness float64
	Agreement    float64
	EntityTier   EntityResolutionTier
	FetchSuccess float64
}

// SearchResult is the merged + deduped discovery result.
type SearchResult struct {
	Kind       ResultKind
	Title      string
	Subtitle   string
	ImageURL   string
	Confidence Confidence
	Sources    []SourceRef
	Extras     map[string]any
	Quality    QualityScore
}

// SearchQuery is the validated user search input.
type SearchQuery struct {
	Raw       string
	QueryNorm string
	Kinds     map[ResultKind]bool
	Limit     int
}

func NewSearchQuery(raw, queryNorm string, kinds map[ResultKind]bool, limit int) (*SearchQuery, error) {
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
		Raw:       raw,
		QueryNorm: queryNorm,
		Kinds:     kinds,
		Limit:     limit,
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

// SearchClick is a persisted click on a discovery result.
type SearchClick struct {
	ID              uuid.UUID
	UserId          shared.UserId
	QueryNorm       string
	ResultSignature string
	Position        int
	Confidence      Confidence
	ClickedAt       time.Time
}

// ProviderSearchResponse wraps a provider's results with metadata.
type ProviderSearchResponse struct {
	Provider ProviderName
	Results  []SearchResult
	Status   ProviderStatus
}
