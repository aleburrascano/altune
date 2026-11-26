package downloadfromlibrary

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/library"
	"context"
)

type OrphanedAudioRecorder struct {
	queue library.OrphanedAudioRecorder
}

func NewOrphanedAudioRecorder(q library.OrphanedAudioRecorder) *OrphanedAudioRecorder {
	return &OrphanedAudioRecorder{queue: q}
}

func (r *OrphanedAudioRecorder) RecordOrphanedAudio(ctx context.Context, orphan download.OrphanedAudio) error {
	return r.queue.RecordOrphanedAudio(ctx, library.OrphanedAudio{
		AudioRef:  orphan.AudioRef,
		UserId:    orphan.UserId,
		TrackId:   orphan.TrackId,
		NotBefore: orphan.NotBefore,
	})
}
