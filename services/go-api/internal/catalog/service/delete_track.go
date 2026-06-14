package service

import (
	"context"
	"errors"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
)

var ErrTrackNotFound = errors.New("track not found")

type DeleteTrackService struct {
	trackRepo ports.TrackRepository
}

func NewDeleteTrackService(trackRepo ports.TrackRepository) *DeleteTrackService {
	return &DeleteTrackService{trackRepo: trackRepo}
}

func (s *DeleteTrackService) Execute(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error {
	deleted, err := s.trackRepo.Delete(ctx, trackId, userId)
	if err != nil {
		return err
	}
	if !deleted {
		return ErrTrackNotFound
	}
	return nil
}
