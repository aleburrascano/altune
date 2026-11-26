package ports

import (
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"time"
)

var ErrOrphanedAudioQueueUnavailable = errors.New("orphaned audio queue unavailable")

type OrphanedAudio struct {
	AudioRef   string
	UserId     ids.UserId
	TrackId    domain.TrackId
	RecordedAt time.Time
	NotBefore  time.Time
	Attempts   int
}

type OrphanedAudioRecorder interface {
	RecordOrphanedAudio(ctx context.Context, orphan OrphanedAudio) error
}

type AudioUsage int

const (
	AudioUnused AudioUsage = iota
	AudioReferenced
	AudioOwnerAcquiring
)

type OrphanedAudioQueue interface {
	OrphanedAudioRecorder
	ListOrphanedAudio(ctx context.Context, limit int) ([]OrphanedAudio, error)
	AudioUsage(ctx context.Context, audioRef string, owner ids.UserId) (AudioUsage, error)
	ResolveOrphanedAudio(ctx context.Context, audioRef string) error
	MarkOrphanedAudioAttempt(ctx context.Context, audioRef string, cause string) error
}
