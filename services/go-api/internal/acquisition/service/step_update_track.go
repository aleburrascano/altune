package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"log/slog"
)

type UpdateTrackStep struct {
	trackRepo ports.TrackRepository
	userId    shared.UserId
	trackId   domain.TrackId
}

func NewUpdateTrackStep(trackRepo ports.TrackRepository, userId shared.UserId, trackId domain.TrackId) *UpdateTrackStep {
	return &UpdateTrackStep{
		trackRepo: trackRepo,
		userId:    userId,
		trackId:   trackId,
	}
}

func (s *UpdateTrackStep) Name() string { return stepNameUpdateTrack }

func (s *UpdateTrackStep) Execute(ctx context.Context, ac *AcquisitionContext, _ afterStore) (afterUpdate, error) {
	return afterUpdate{}, loadAndUpdate(ctx, s.trackRepo, s.trackId, s.userId, errors.New("track not found for update"), func(track *domain.Track) error {
		if err := settleAudio(track, ac); err != nil {
			return err
		}
		if duration := ac.MeasuredDuration(); duration > 0 {
			// An implausible probe (ffprobe "inf", a bogus provider value) must
			// not fail an otherwise good acquisition: keep the duration unknown.
			if err := track.SetDuration(duration); err != nil {
				slog.WarnContext(ctx, "acquisition.duration_rejected",
					"track_id", track.ID.String(), "duration", duration, "error", logSafeError(err))
			}
		}
		track.SetAcquisitionProvenance(ac.Provenance())
		for _, key := range ac.Replace.ExcludeKeys {
			track.RejectAudioSource(key)
		}
		if ac.Selected != nil {
			track.SetAudioSource(ac.Selected.URL)
		}
		return nil
	})
}

// settleAudio records the acquired audio: a replace swaps the audio the track
// had when the job started, any other run completes the acquisition.
func settleAudio(track *domain.Track, ac *AcquisitionContext) error {
	if ac.Replace.PreservedRef != "" {
		if err := track.ReplaceAudio(ac.AudioRef); err != nil {
			return fmt.Errorf("replace audio: %w", err)
		}
		return nil
	}
	if err := track.MarkReady(ac.AudioRef); err != nil {
		return fmt.Errorf("mark ready: %w", err)
	}
	return nil
}

// Rollback reverts the track to pending. A track already pending has nothing
// to undo, so the refused transition is not reported as a rollback failure.
func (s *UpdateTrackStep) Rollback(ctx context.Context, _ *AcquisitionContext) error {
	err := loadAndUpdate(ctx, s.trackRepo, s.trackId, s.userId, nil, func(track *domain.Track) error {
		return track.RevertToPending()
	})
	if errors.Is(err, domain.ErrIllegalAcquisitionTransition) {
		return nil
	}
	return err
}
