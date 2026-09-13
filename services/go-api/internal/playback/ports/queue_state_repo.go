package ports

import (
	"context"
	"errors"

	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/shared"
)

// ErrCorruptStoredState classifies a stored queue row that exists but can no
// longer be rehydrated into a valid domain.QueueState (e.g. an unknown repeat
// mode or a track list over the domain maximum). It is a server-side data
// fault, not a client error. Match it with errors.Is.
var ErrCorruptStoredState = errors.New("corrupt stored queue state")

type QueueStateRepository interface {
	Upsert(ctx context.Context, state *domain.QueueState) error
	// GetForUser returns (nil, nil) when nothing is stored, and an error that
	// satisfies errors.Is(err, ErrCorruptStoredState) when the stored row is
	// present but invalid.
	GetForUser(ctx context.Context, userId shared.UserId) (*domain.QueueState, error)
	// DeleteForUser erases the user's persisted queue state (PII: track list,
	// natural order, and free-text search source_id). Idempotent: deleting a
	// user with no stored state is not an error.
	DeleteForUser(ctx context.Context, userId shared.UserId) error
}
