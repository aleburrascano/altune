package ports

import (
	shared "altune/go-api/internal/shared/kernel"
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
