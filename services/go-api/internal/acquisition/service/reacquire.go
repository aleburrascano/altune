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
		if proceed, err := p.reconcileReady(ctx, track); !proceed {
			return false, err
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

// reconcileReady decides whether a Ready track must be re-acquired by checking
// its stored file still exists. proceed is true when the file is missing (or
// there is no AudioRef), meaning the caller should revert and re-acquire; it is
// false when the file exists (err nil) or the check itself failed (err set).
func (p reacquirePolicy) reconcileReady(ctx context.Context, track *domain.Track) (proceed bool, err error) {
	if track.AudioRef == nil {
		return true, nil
	}
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
		return true, nil
	}
}

func (p reacquirePolicy) revertToPending(ctx context.Context, track *domain.Track) error {
	if err := track.RevertToPending(); err != nil {
		return fmt.Errorf("revert to pending: %w", err)
	}
	if err := p.trackRepo.Update(ctx, track); err != nil {
		return fmt.Errorf("revert to pending: %w", err)
	}
	return nil
}
