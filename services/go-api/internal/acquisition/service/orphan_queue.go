package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/redact"
	"context"
	"log/slog"
	"time"
)

const orphanRecordTimeout = 5 * time.Second

func recordOrphanedAudio(ctx context.Context, q ports.OrphanedAudioRecorder, userId shared.UserId, trackId shared.TrackId, audioRef string) {
	if q == nil {
		return
	}
	_ = queueOrphanedAudio(ctx, q, ports.OrphanedAudio{AudioRef: audioRef, UserId: userId, TrackId: trackId})
}

func queueOrphanedAudio(ctx context.Context, q ports.OrphanedAudioRecorder, orphan ports.OrphanedAudio) error {
	recordCtx, cancel := detachedTimeout(ctx, orphanRecordTimeout)
	defer cancel()
	err := q.RecordOrphanedAudio(recordCtx, orphan)
	if err != nil {
		slog.ErrorContext(ctx, "acquisition.orphaned_audio_not_queued",
			"track_id", orphan.TrackId.String(), "audio_ref", orphan.AudioRef, "error", redact.LogError(err))
	}
	return err
}
