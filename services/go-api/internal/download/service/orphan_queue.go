package service

import (
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/redact"
	"context"
	"log/slog"
	"time"
)

const orphanRecordTimeout = 5 * time.Second

func recordOrphanedAudio(ctx context.Context, q ports.OrphanedAudioRecorder, userId ids.UserId, trackId ids.TrackId, audioRef string) {
	if q == nil {
		return
	}
	_ = queueOrphanedAudio(ctx, q, ports.OrphanedAudio{AudioRef: audioRef, UserId: userId, TrackId: trackId})
}

func queueOrphanedAudio(ctx context.Context, q ports.OrphanedAudioRecorder, orphan ports.OrphanedAudio) error {
	recordCtx, cancel := background.Detach(ctx, orphanRecordTimeout)
	defer cancel()
	err := q.RecordOrphanedAudio(recordCtx, orphan)
	if err != nil {
		slog.ErrorContext(ctx, "acquisition.orphaned_audio_not_queued",
			"track_id", orphan.TrackId.String(), "audio_ref", orphan.AudioRef, "error", redact.LogError(err))
	}
	return err
}
