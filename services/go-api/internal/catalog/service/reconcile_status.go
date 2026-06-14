package service

import (
	"context"
	"log/slog"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
)

type ReconcileTrackStatusService struct {
	trackRepo  ports.TrackRepository
	audioStore ports.AudioStore
}

func NewReconcileTrackStatusService(trackRepo ports.TrackRepository, audioStore ports.AudioStore) *ReconcileTrackStatusService {
	return &ReconcileTrackStatusService{trackRepo: trackRepo, audioStore: audioStore}
}

func (s *ReconcileTrackStatusService) Execute(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error {
	track, err := s.trackRepo.GetByID(ctx, trackId, userId)
	if err != nil {
		return err
	}
	if track == nil {
		return ErrTrackNotFound
	}

	if track.AcquisitionStatus != domain.AcquisitionReady || track.AudioRef == nil {
		return nil
	}

	exists, err := s.audioStore.Exists(ctx, *track.AudioRef)
	if err != nil {
		slog.WarnContext(ctx, "audio store check failed during reconciliation",
			"track_id", trackId.String(), "error", err)
		return nil
	}

	if exists {
		return nil
	}

	if err := track.MarkFailed("audio file missing from storage"); err != nil {
		return err
	}

	slog.WarnContext(ctx, "track marked failed: audio file missing",
		"track_id", trackId.String(), "user_id", userId.String())

	return s.trackRepo.Update(ctx, track)
}
