package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/redact"
	"context"
	"fmt"
	"log/slog"
	"time"
)

const orphanRecordTimeout = 5 * time.Second

type audioDeleter interface {
	Delete(ctx context.Context, audioRef string) error
}

type directDeleteOrphans struct{ store audioDeleter }

func DirectDeleteOrphans(store audioDeleter) (ports.OrphanedAudioRecorder, error) {
	if err := missingPort("DirectDeleteOrphans", "audio store", store); err != nil {
		return nil, err
	}
	return directDeleteOrphans{store: store}, nil
}

func MustDirectDeleteOrphans(store audioDeleter) ports.OrphanedAudioRecorder {
	recorder, err := DirectDeleteOrphans(store)
	if err != nil {
		panic(err)
	}
	return recorder
}

func (d directDeleteOrphans) RecordOrphanedAudio(ctx context.Context, orphan domain.OrphanedAudio) error {
	if err := d.store.Delete(ctx, orphan.AudioRef); err != nil {
		return fmt.Errorf("delete orphaned audio: %w", err)
	}
	return nil
}

func recordOrphanedAudio(ctx context.Context, q ports.OrphanedAudioRecorder, userId ids.UserId, trackId ids.TrackId, audioRef string) {
	_ = queueOrphanedAudio(ctx, q, domain.OrphanedAudio{AudioRef: audioRef, UserId: userId, TrackId: trackId})
}

func queueOrphanedAudio(ctx context.Context, q ports.OrphanedAudioRecorder, orphan domain.OrphanedAudio) error {
	recordCtx, cancel := background.Detach(ctx, orphanRecordTimeout)
	defer cancel()
	err := q.RecordOrphanedAudio(recordCtx, orphan)
	if err != nil {
		slog.ErrorContext(ctx, "acquisition.orphaned_audio_not_queued",
			logging.KeyTrackID, orphan.TrackId.String(), "audio_ref", orphan.AudioRef, logging.KeyError, redact.LogError(err))
	}
	return err
}
