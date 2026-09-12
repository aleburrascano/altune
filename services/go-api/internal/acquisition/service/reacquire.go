package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"context"
	"fmt"
	"log/slog"
)

// reacquirePolicy decides whether a track should run through acquisition again
// and reverts its persisted state to pending when it should. It owns the two
// collaborators that decision needs: the track repository (to persist a revert)
// and the audio store (to confirm a supposedly-ready file still exists).
type reacquirePolicy struct {
	trackRepo  ports.TrackRepository
	audioStore ports.AudioWriter
}

func (p reacquirePolicy) reconcile(ctx context.Context, track *domain.Track) (proceed bool, err error) {
	switch track.AcquisitionStatus {
	case domain.AcquisitionReady:
		if track.AudioRef != nil {
			exists, existsErr := p.audioStore.Exists(ctx, *track.AudioRef)
			switch {
			case existsErr != nil:
				slog.WarnContext(ctx, "acquire_exists_check_failed",
					"track_id", track.ID.String(), "audio_ref", *track.AudioRef, "error", existsErr)
				// A transient exists-check error is not evidence the file is gone;
				// bail out rather than clearing a still-good AudioRef via revert.
				return false, fmt.Errorf("reconcile exists check: %w", existsErr)
			case exists:
				slog.InfoContext(ctx, "acquire_skip_already_ready", "track_id", track.ID.String())
				return false, nil
			default:
				slog.InfoContext(ctx, "acquire_reacquire_missing_file",
					"track_id", track.ID.String(), "audio_ref", *track.AudioRef)
			}
		}
		if err := p.revertToPending(ctx, track); err != nil {
			return false, err
		}
	case domain.AcquisitionFailed:
		slog.InfoContext(ctx, "acquire_retrying_failed", "track_id", track.ID.String())
		if err := p.revertToPending(ctx, track); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (p reacquirePolicy) revertToPending(ctx context.Context, track *domain.Track) error {
	track.RevertToPending()
	if err := p.trackRepo.Update(ctx, track); err != nil {
		return fmt.Errorf("revert to pending: %w", err)
	}
	return nil
}
