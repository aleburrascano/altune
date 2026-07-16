package service

import (
	"context"
	"fmt"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
)

// SetTrackNumberService persists a track's album position when it is unset.
// Backs the client's persist-as-you-browse flow: when the album detail derives a
// track's real position from the album tracklist, it writes it back so a track
// saved before track_number was captured is corrected in the database. Fill-only
// at the repository, so re-running never overwrites a real value.
type SetTrackNumberService struct {
	trackRepo ports.TrackRepository
}

func NewSetTrackNumberService(trackRepo ports.TrackRepository) *SetTrackNumberService {
	return &SetTrackNumberService{trackRepo: trackRepo}
}

// Execute fills the track's position when unset. `updated` reports whether a row
// changed (false when the track already had a number or was not found — both are
// non-errors for an idempotent backfill).
func (s *SetTrackNumberService) Execute(
	ctx context.Context,
	userId shared.UserId,
	trackId domain.TrackId,
	trackNumber int,
) (updated bool, err error) {
	if trackNumber <= 0 {
		return false, &domain.ValidationError{Message: "track_number must be positive"}
	}
	updated, err = s.trackRepo.SetTrackNumber(ctx, trackId, userId, trackNumber)
	if err != nil {
		return false, fmt.Errorf("set track number: %w", err)
	}
	return updated, nil
}
