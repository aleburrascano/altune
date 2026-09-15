package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"fmt"
)

type SetTrackNumberService struct {
	trackRepo ports.TrackNumberFiller
}

func NewSetTrackNumberService(trackRepo ports.TrackNumberFiller) *SetTrackNumberService {
	return &SetTrackNumberService{trackRepo: trackRepo}
}

// Execute validates trackNumber and fills the track's album position once.
//
// The write is write-once (see ports.TrackNumberSetter): a track that already
// has a number keeps it, and that no-op returns updated=false with a nil error;
// callers must not treat it as a failure. When the write is a no-op because no
// track with trackId is owned by userId, Execute returns ErrTrackNotFound. A
// foreign track is reported the same way as a missing one, so its existence is
// not revealed.
func (s *SetTrackNumberService) Execute(
	ctx context.Context,
	userId shared.UserId,
	trackId domain.TrackId,
	trackNumber int,
) (updated bool, err error) {
	if trackNumber <= 0 {
		return false, domain.NewValidationError("track_number must be positive")
	}
	if trackNumber > maxTrackNumber {
		return false, domain.NewValidationError("track_number exceeds maximum (int4)")
	}
	updated, err = s.trackRepo.SetTrackNumber(ctx, trackId, userId, trackNumber)
	if err != nil {
		return false, fmt.Errorf("set track number: %w", err)
	}
	if updated {
		return true, nil
	}
	return false, s.requireOwnedTrack(ctx, userId, trackId)
}

// requireOwnedTrack disambiguates a no-op write: it returns ErrTrackNotFound
// unless userId owns trackId. The lookup runs after the write, so a track
// deleted in between is reported as not found, which is what it now is.
func (s *SetTrackNumberService) requireOwnedTrack(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error {
	track, err := s.trackRepo.GetByID(ctx, trackId, userId)
	if err != nil {
		return fmt.Errorf("set track number: lookup: %w", err)
	}
	if track == nil {
		return ErrTrackNotFound
	}
	return nil
}
