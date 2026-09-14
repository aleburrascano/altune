package domain

import (
	"altune/go-api/internal/shared"
	"time"

	"github.com/google/uuid"
)

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

type RelationshipKind string

const (
	RelationshipLibraryMatches RelationshipKind = "library_matches"
	RelationshipAlbumTracks    RelationshipKind = "album_tracks"
	RelationshipArtistAlbums   RelationshipKind = "artist_albums"
)

type RelatedGroup struct {
	Relationship RelationshipKind
	RelatedTo    string
	Items        []SearchResult
}
