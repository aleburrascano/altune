package service

import (
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/redact"
	"context"
	"log/slog"
	"time"
)

const supersededDeleteBudget = 30 * time.Second

type supersededAudio struct {
	ledger  ports.LibraryTracks
	orphans ports.OrphanedAudioRecorder
	grace   time.Duration
	now     clock.Func
}

func (a supersededAudio) release(ctx context.Context, userId ids.UserId, trackId ids.TrackId, ac *AcquisitionContext) {
	old := ac.Replace.PreservedRef
	if old == "" || old == ac.AudioRef {
		return
	}
	delCtx, cancel := background.Detach(ctx, supersededDeleteBudget)
	defer cancel()
	if a.servedByAnotherTrack(delCtx, trackId, old) {
		return
	}
	a.handToRecorder(delCtx, userId, trackId, old)
}

func (a supersededAudio) handToRecorder(ctx context.Context, userId ids.UserId, trackId ids.TrackId, old string) {
	err := queueOrphanedAudio(ctx, a.orphans, ports.OrphanedAudio{
		AudioRef:  old,
		UserId:    userId,
		TrackId:   trackId,
		NotBefore: a.now().Add(a.grace),
	})
	if err != nil {
		return
	}
	slog.InfoContext(ctx, "acquisition.replace_superseded_audio_released",
		logging.KeyTrackID, trackId.String(), "audio_ref", old, "grace", a.grace.String())
}

func (a supersededAudio) servedByAnotherTrack(ctx context.Context, trackId ids.TrackId, audioRef string) bool {
	inUse, err := a.ledger.AudioRefInUse(ctx, audioRef, trackId)
	if err != nil {
		slog.ErrorContext(ctx, "acquisition.replace_audio_usage_unknown",
			logging.KeyTrackID, trackId.String(), "audio_ref", audioRef, logging.KeyError, redact.LogError(err))
		return true
	}
	if inUse {
		slog.InfoContext(ctx, "acquisition.replace_kept_shared_audio",
			logging.KeyTrackID, trackId.String(), "audio_ref", audioRef)
	}
	return inUse
}
