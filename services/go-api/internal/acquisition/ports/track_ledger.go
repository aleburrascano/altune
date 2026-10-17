package ports

import (
	"altune/go-api/internal/shared"
	"context"
	"encoding/json"
	"errors"
)

type TrackStatus int

const (
	TrackPending TrackStatus = iota
	TrackReady
	TrackFailed
)

var (
	ErrIllegalTransition = errors.New("illegal acquisition status transition")
	ErrTrackNotFound     = errors.New("track not found")
)

type CatalogSource struct {
	Provider   string
	ExternalID string
	URL        string
}

type TrackSnapshot struct {
	ID                     shared.TrackId
	UserId                 shared.UserId
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

type RecordingLabel struct {
	MBID            string
	Title           string
	Album           string
	Year            int
	ISRC            string
	DurationSeconds float64
}

type Completion struct {
	AudioRef           string
	ReplaceAudio       bool
	Duration           float64
	ISRC               string
	Provenance         shared.AcquisitionProvenance
	RejectedSourceKeys []string
	SourceURL          string
	Confidence         float64
	Evidence           json.RawMessage
	Relabel            *RecordingLabel
}

type TrackLedger interface {
	AudioRefLookup
	Get(ctx context.Context, id shared.TrackId, userId shared.UserId) (*TrackSnapshot, error)
	Complete(ctx context.Context, id shared.TrackId, userId shared.UserId, fence Fence, c Completion) error
	Fail(ctx context.Context, id shared.TrackId, userId shared.UserId, fence Fence, reason string) error
	RevertAt(ctx context.Context, track TrackSnapshot) error
}
