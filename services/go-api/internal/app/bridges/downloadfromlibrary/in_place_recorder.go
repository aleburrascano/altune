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

type InPlaceStateStore interface {
	RecordInPlaceState(ctx context.Context, trackID ids.TrackId, generation int, basis string, bitErrorRate float64) error
	NoteInPlaceFailure(ctx context.Context, trackID ids.TrackId) (int, error)
}

type InPlaceRecorder struct {
	verdicts VerdictStore
	state    InPlaceStateStore
}

var _ download.InPlaceStore = (*InPlaceRecorder)(nil)

func NewInPlaceRecorder(verdicts VerdictStore, state InPlaceStateStore) *InPlaceRecorder {
	return &InPlaceRecorder{verdicts: verdicts, state: state}
}

func (r *InPlaceRecorder) RecordInPlace(ctx context.Context, trackID ids.TrackId, record download.InPlaceRecord) (bool, error) {
	recorded, err := r.verdicts.RecordInPlaceVerdict(ctx, trackID, record.AudioRef, library.InPlaceVerdict{
		Verdict:            record.Verdict,
		Score:              record.Score,
		Grade:              record.Grade,
		UserUploadMismatch: record.UserUploadMismatch,
		MayLower:           record.MayLower,
		DropScore:          record.Verdict == "preview",
	})
	if err != nil || !recorded {
		return false, err
	}
	if err := r.state.RecordInPlaceState(ctx, trackID, record.Generation, record.Basis, record.BitErrorRate); err != nil {
		return false, fmt.Errorf("record in-place state: %w", err)
	}
	return true, nil
}

func (r *InPlaceRecorder) NoteInPlaceFailure(ctx context.Context, trackID ids.TrackId) (int, error) {
	return r.state.NoteInPlaceFailure(ctx, trackID)
}
