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

type TrackSnapshot struct {
	ID                 shared.TrackId
	UserId             shared.UserId
	Title              string
	Artist             string
	Album              string
	DurationSeconds    *float64
	ISRC               *string
	Year               *int
	TrackNumber        *int
	AlbumArtist        *string
	Genre              *string
	AudioRef           *string
	AudioSourceURL     *string
	RejectedSourceKeys []string
	Status             TrackStatus
	Streamable         bool
	Version            int
}

type Completion struct {
	AudioRef           string
	ReplaceAudio       bool
	Duration           float64
	Provenance         shared.AcquisitionProvenance
	RejectedSourceKeys []string
	SourceURL          string
	Confidence         float64
	Evidence           json.RawMessage
}

type TrackLedger interface {
	AudioRefLookup
	Get(ctx context.Context, id shared.TrackId, userId shared.UserId) (*TrackSnapshot, error)
	Complete(ctx context.Context, id shared.TrackId, userId shared.UserId, c Completion) error
	Fail(ctx context.Context, id shared.TrackId, userId shared.UserId, reason string) error
	Revert(ctx context.Context, id shared.TrackId, userId shared.UserId) error
	RevertAt(ctx context.Context, track TrackSnapshot) error
}
