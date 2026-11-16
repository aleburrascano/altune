package catalogbridge

import (
	"altune/go-api/internal/acquisition/internal/core/ports"
	"altune/go-api/internal/catalog"
	"context"
)

type OrphanedAudioRecorder struct {
	queue catalog.OrphanedAudioRecorder
}

func NewOrphanedAudioRecorder(q catalog.OrphanedAudioRecorder) *OrphanedAudioRecorder {
	return &OrphanedAudioRecorder{queue: q}
}

func (r *OrphanedAudioRecorder) RecordOrphanedAudio(ctx context.Context, orphan ports.OrphanedAudio) error {
	return r.queue.RecordOrphanedAudio(ctx, catalog.OrphanedAudio{
		AudioRef:  orphan.AudioRef,
		UserId:    orphan.UserId,
		TrackId:   orphan.TrackId,
		NotBefore: orphan.NotBefore,
	})
}
