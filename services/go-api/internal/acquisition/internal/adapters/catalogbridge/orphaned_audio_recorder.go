package catalogbridge

import (
	"altune/go-api/internal/acquisition/ports"
	catalogports "altune/go-api/internal/catalog/ports"
	"context"
)

type OrphanedAudioRecorder struct {
	queue catalogports.OrphanedAudioRecorder
}

func NewOrphanedAudioRecorder(q catalogports.OrphanedAudioRecorder) *OrphanedAudioRecorder {
	return &OrphanedAudioRecorder{queue: q}
}

func (r *OrphanedAudioRecorder) RecordOrphanedAudio(ctx context.Context, orphan ports.OrphanedAudio) error {
	return r.queue.RecordOrphanedAudio(ctx, catalogports.OrphanedAudio{
		AudioRef:  orphan.AudioRef,
		UserId:    orphan.UserId,
		TrackId:   orphan.TrackId,
		NotBefore: orphan.NotBefore,
	})
}
