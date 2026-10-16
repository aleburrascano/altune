package ports

import (
	"altune/go-api/internal/shared"
	"context"
	"time"
)

type OrphanedAudio struct {
	AudioRef  string
	UserId    shared.UserId
	TrackId   shared.TrackId
	NotBefore time.Time
}

type OrphanedAudioRecorder interface {
	RecordOrphanedAudio(ctx context.Context, orphan OrphanedAudio) error
}
