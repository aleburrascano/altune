package ports

import (
	"altune/go-api/internal/shared"
	"context"
	"time"
)

type UpgradeCandidate struct {
	TrackID shared.TrackId
	UserID  shared.UserId
	Grade   string
}

type UpgradeSelection struct {
	Limit      int
	Generation int
	RetryAfter time.Duration
	MaxQueued  int

	IncludeUnverifiedUploads bool
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
	Grade              string
	UserUploadMismatch bool
}

type InPlaceStore interface {
	RecordInPlace(ctx context.Context, trackID shared.TrackId, record InPlaceRecord) (recorded bool, err error)
}

type UpgradeStore interface {
	Candidates(ctx context.Context, selection UpgradeSelection) ([]UpgradeCandidate, error)
	NoteAttempt(ctx context.Context, trackID shared.TrackId, attempt UpgradeAttempt) (tries int, err error)
	GiveUp(ctx context.Context, trackID shared.TrackId, generation int, reason string) error
}
