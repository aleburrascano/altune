package service

import (
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"fmt"
)

type InPlaceRecorder struct {
	verdicts ports.InPlaceVerdicts
	state    ports.InPlaceState
}

var _ ports.InPlaceStore = (*InPlaceRecorder)(nil)

var errInPlaceRecorderPortMissing = errors.New("download: the in-place recorder needs the library verdicts and the upgrade state")

func NewInPlaceRecorder(verdicts ports.InPlaceVerdicts, state ports.InPlaceState) (*InPlaceRecorder, error) {
	if verdicts == nil || state == nil {
		return nil, errInPlaceRecorderPortMissing
	}
	return &InPlaceRecorder{verdicts: verdicts, state: state}, nil
}

func (r *InPlaceRecorder) RecordInPlace(ctx context.Context, trackID ids.TrackId, record ports.InPlaceRecord) (bool, error) {
	recorded, err := r.verdicts.RecordInPlaceVerdict(ctx, trackID, record)
	if err != nil {
		return false, fmt.Errorf("record in-place verdict: %w", err)
	}
	if !recorded {
		return false, nil
	}
	if err := r.state.RecordInPlaceState(ctx, trackID, record.Generation, record.Basis, record.BitErrorRate); err != nil {
		return false, fmt.Errorf("record in-place state: %w", err)
	}
	return true, nil
}

func (r *InPlaceRecorder) NoteInPlaceFailure(ctx context.Context, trackID ids.TrackId) (int, error) {
	failures, err := r.state.NoteInPlaceFailure(ctx, trackID)
	if err != nil {
		return failures, fmt.Errorf("note in place failure: %w", err)
	}
	return failures, nil
}
