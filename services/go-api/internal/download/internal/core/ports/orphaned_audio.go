package ports

import (
	"altune/go-api/internal/shared/ids"
	"context"
	"time"
)

type OrphanedAudio struct {
	AudioRef  string
	UserId    ids.UserId
	TrackId   ids.TrackId
	NotBefore time.Time
}

type OrphanedAudioRecorder interface {
	RecordOrphanedAudio(ctx context.Context, orphan OrphanedAudio) error
}
