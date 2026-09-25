package service

import (
	"altune/go-api/internal/catalog/domain"
	catalogports "altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"log/slog"
	"time"
)

const orphanRecordTimeout = 5 * time.Second

func recordOrphanedAudio(ctx context.Context, q catalogports.OrphanedAudioRecorder, userId shared.UserId, trackId domain.TrackId, audioRef string) {
	if q == nil {
		return
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), orphanRecordTimeout)
	defer cancel()
	err := q.RecordOrphanedAudio(recordCtx, catalogports.OrphanedAudio{AudioRef: audioRef, UserId: userId, TrackId: trackId})
	if err != nil {
		slog.ErrorContext(ctx, "acquisition.orphaned_audio_not_queued",
			"track_id", trackId.String(), "audio_ref", audioRef, "error", logSafeError(err))
	}
}
