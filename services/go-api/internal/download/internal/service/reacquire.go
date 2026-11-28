package service

import (
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/logging"
	"context"
	"fmt"
	"log/slog"
)

type reacquirePolicy struct {
	ledger     ports.LibraryTracks
	audioStore ports.AudioWriter
}

func (p reacquirePolicy) reconcile(ctx context.Context, track *ports.TrackSnapshot) (proceed bool, err error) {
	switch track.Status {
	case ports.TrackReady:
		if proceed, err := p.reconcileReady(ctx, track); !proceed {
			return false, err
		}
		if err := p.revertToPending(ctx, track); err != nil {
			return false, err
		}
	case ports.TrackPending:
	case ports.TrackFailed:
		slog.InfoContext(ctx, "acquire_retrying_failed", logging.KeyTrackID, track.ID.String())
		if err := p.revertToPending(ctx, track); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (p reacquirePolicy) reconcileReady(ctx context.Context, track *ports.TrackSnapshot) (proceed bool, err error) {
	if track.AudioRef == nil {
		return true, nil
	}
	exists, existsErr := p.audioStore.Exists(ctx, *track.AudioRef)
	switch {
	case existsErr != nil:
		slog.WarnContext(ctx, "acquire_exists_check_failed",
			logging.KeyTrackID, track.ID.String(), "audio_ref", *track.AudioRef, logging.KeyError, existsErr)
		return false, fmt.Errorf("reconcile exists check: %w", existsErr)
	case exists:
		slog.InfoContext(ctx, "acquire_skip_already_ready", logging.KeyTrackID, track.ID.String())
		return false, nil
	default:
		slog.InfoContext(ctx, "acquire_reacquire_missing_file",
			logging.KeyTrackID, track.ID.String(), "audio_ref", *track.AudioRef)
		return true, nil
	}
}

func (p reacquirePolicy) revertToPending(ctx context.Context, track *ports.TrackSnapshot) error {
	if err := p.ledger.RevertAt(ctx, *track); err != nil {
		return fmt.Errorf("revert to pending: %w", err)
	}
	return nil
}
