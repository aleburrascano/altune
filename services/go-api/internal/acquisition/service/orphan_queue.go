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
	recordCtx, cancel := detachedTimeout(ctx, orphanRecordTimeout)
	defer cancel()
	err := q.RecordOrphanedAudio(recordCtx, ports.OrphanedAudio{AudioRef: audioRef, UserId: userId, TrackId: trackId})
	if err != nil {
		slog.ErrorContext(ctx, "acquisition.orphaned_audio_not_queued",
			"track_id", trackId.String(), "audio_ref", audioRef, "error", redact.LogError(err))
	}
}
