package ports

import (
	"altune/go-api/internal/shared/ids"
	"context"
	"time"
)

type UpgradeCandidate struct {
	TrackID ids.TrackId
	UserID  ids.UserId
	Grade   string
}

type UpgradeSelection struct {
	Limit      int
	Generation int
	RetryAfter time.Duration
	MaxQueued  int

	CheckInPlace bool

	LowBitrateFloorKbps int
}

type UpgradeAttempt struct {
	RejectedSourceKey string
	CountsAsTry       bool
}

type InPlaceRecord struct {
	AudioRef           string
	Generation         int
	Verdict            string
	Basis              string
	Score              float64
	BitErrorRate       float64
	Grade              string
	UserUploadMismatch bool
	MayLower           bool
}

type InPlaceStore interface {
	RecordInPlace(ctx context.Context, trackID ids.TrackId, record InPlaceRecord) (recorded bool, err error)
	NoteInPlaceFailure(ctx context.Context, trackID ids.TrackId) (failures int, err error)
}

type UpgradeStore interface {
	Candidates(ctx context.Context, selection UpgradeSelection) ([]UpgradeCandidate, error)
	NoteAttempt(ctx context.Context, trackID ids.TrackId, attempt UpgradeAttempt) (tries int, err error)
	GiveUp(ctx context.Context, trackID ids.TrackId, generation int, reason string) error
}
