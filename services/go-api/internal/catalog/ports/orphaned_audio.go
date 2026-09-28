package ports

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"time"
)

var ErrOrphanedAudioQueueUnavailable = errors.New("orphaned audio queue unavailable")

type OrphanedAudio struct {
	AudioRef   string
	UserId     shared.UserId
	TrackId    domain.TrackId
	RecordedAt time.Time
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
	AudioUsage(ctx context.Context, audioRef string, owner shared.UserId) (AudioUsage, error)
	ResolveOrphanedAudio(ctx context.Context, audioRef string) error
	MarkOrphanedAudioAttempt(ctx context.Context, audioRef string, cause string) error
}
