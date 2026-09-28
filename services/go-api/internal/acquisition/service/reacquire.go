package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"context"
	"fmt"
	"log/slog"
)

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

func (p reacquirePolicy) reconcileReady(ctx context.Context, track *domain.Track) (proceed bool, err error) {
	if track.AudioRef == nil {
		return true, nil
	}
	exists, existsErr := p.audioStore.Exists(ctx, *track.AudioRef)
	switch {
	case existsErr != nil:
		slog.WarnContext(ctx, "acquire_exists_check_failed",
			"track_id", track.ID.String(), "audio_ref", *track.AudioRef, "error", existsErr)
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
	expectedVersion := track.Version
	if err := track.RevertToPending(); err != nil {
		return fmt.Errorf("revert to pending: %w", err)
	}
	if err := p.trackRepo.Update(ctx, track, expectedVersion); err != nil {
		return fmt.Errorf("revert to pending: %w", err)
	}
	return nil
}
