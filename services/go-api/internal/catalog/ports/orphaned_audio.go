package ports

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"time"
)

// ErrOrphanedAudioQueueUnavailable reports that the durable orphaned-audio
// queue does not exist yet (its migration has not been applied). Callers
// degrade to the pre-queue behaviour (log + metric only) instead of failing.
var ErrOrphanedAudioQueueUnavailable = errors.New("orphaned audio queue unavailable")

// OrphanedAudio is one audio object left in storage after its track row was
// deleted and the storage delete failed, pending a retried cleanup.
type OrphanedAudio struct {
	AudioRef   string
	UserId     shared.UserId
	TrackId    domain.TrackId
	RecordedAt time.Time
	Attempts   int
}

// OrphanedAudioRecorder durably records an orphaned audio object so a sweep
// can retry its deletion.
type OrphanedAudioRecorder interface {
	RecordOrphanedAudio(ctx context.Context, orphan OrphanedAudio) error
}

// AudioUsage is whether a recorded orphan's storage key may be deleted.
type AudioUsage int

const (
	// AudioUnused: no track references the key and nothing may be about to.
	AudioUnused AudioUsage = iota
	// AudioReferenced: some track (of any user) references the key, so it is
	// no longer an orphan.
	AudioReferenced
	// AudioOwnerAcquiring: the owner has a pending acquisition, which may be
	// about to store to and commit that same canonical key.
	AudioOwnerAcquiring
)

// OrphanedAudioQueue is the sweep's view of the recorded orphans.
//
// AudioUsage is the safety gate: audio keys are shared between tracks with
// equivalent metadata, so the sweep must never delete an object unless
// AudioUsage returned AudioUnused without error.
type OrphanedAudioQueue interface {
	OrphanedAudioRecorder
	ListOrphanedAudio(ctx context.Context, limit int) ([]OrphanedAudio, error)
	AudioUsage(ctx context.Context, audioRef string, owner shared.UserId) (AudioUsage, error)
	ResolveOrphanedAudio(ctx context.Context, audioRef string) error
	MarkOrphanedAudioAttempt(ctx context.Context, audioRef string, cause string) error
}
