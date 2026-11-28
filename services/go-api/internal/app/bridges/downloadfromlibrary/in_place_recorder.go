package downloadfromlibrary

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/library"
	"altune/go-api/internal/shared/ids"
	"context"
	"fmt"
)

type VerdictStore interface {
	RecordInPlaceVerdict(ctx context.Context, id ids.TrackId, audioRef string, v library.InPlaceVerdict) (bool, error)
}

type InPlaceVerdicts struct {
	verdicts VerdictStore
}

var _ download.InPlaceVerdicts = (*InPlaceVerdicts)(nil)

func NewInPlaceVerdicts(verdicts VerdictStore) *InPlaceVerdicts {
	return &InPlaceVerdicts{verdicts: verdicts}
}

func (r *InPlaceVerdicts) RecordInPlaceVerdict(ctx context.Context, trackID ids.TrackId, record download.InPlaceRecord) (bool, error) {
	recorded, err := r.verdicts.RecordInPlaceVerdict(ctx, trackID, record.AudioRef, library.InPlaceVerdict{
		Verdict:            record.Verdict,
		Score:              record.Score,
		Grade:              record.Grade,
		UserUploadMismatch: record.UserUploadMismatch,
		MayLower:           record.MayLower,
		DropScore:          record.Verdict == "preview",
	})
	if err != nil {
		return false, fmt.Errorf("record in-place verdict: %w", err)
	}
	return recorded, nil
}
