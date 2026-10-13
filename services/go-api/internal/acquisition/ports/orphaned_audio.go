package ports

import (
	"altune/go-api/internal/shared"
	"context"
)

type OrphanedAudio struct {
	AudioRef string
	UserId   shared.UserId
	TrackId  shared.TrackId
}

type OrphanedAudioRecorder interface {
	RecordOrphanedAudio(ctx context.Context, orphan OrphanedAudio) error
}
