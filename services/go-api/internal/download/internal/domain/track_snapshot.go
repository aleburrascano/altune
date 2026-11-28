package domain

import (
	"altune/go-api/internal/shared/ids"
	"encoding/json"
)

type CatalogSource struct {
	Provider   string
	ExternalID string
	URL        string
}

type TrackSnapshot struct {
	ID                     ids.TrackId
	UserId                 ids.UserId
	Title                  string
	Artist                 string
	Album                  string
	CatalogDurationSeconds *float64
	CatalogMBID            *string
	CatalogExplicit        *bool
	CatalogSources         []CatalogSource
	ISRC                   *string
	Year                   *int
	TrackNumber            *int
	AlbumArtist            *string
	Genre                  *string
	AudioRef               *string
	AudioSourceURL         *string
	StoredDurationSeconds  *float64
	RejectedSourceKeys     []string
	Evidence               json.RawMessage
	Status                 TrackStatus
	Streamable             bool
	Version                int
}
