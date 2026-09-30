package ports

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"time"
)

type JobKind string

const (
	JobKindAcquire JobKind = "acquire"
	JobKindReplace JobKind = "replace"
)

var (
	ErrNoJobAvailable  = errors.New("no acquisition job available")
	ErrLeaseLost       = errors.New("acquisition lease lost")
	ErrJobKindConflict = errors.New("acquisition job of a different kind is already in flight")
)

type Job struct {
	TrackID  domain.TrackId
	UserID   shared.UserId
	Kind     JobKind
	Attempts int
	Run      int
	Fence    Fence
}

type Fence int

type JobQueue interface {
	Enqueue(ctx context.Context, trackID domain.TrackId, kind JobKind, availableAt time.Time) error
	Claim(ctx context.Context, lease time.Duration) (Job, error)
	Heartbeat(ctx context.Context, trackID domain.TrackId, fence Fence, lease time.Duration) error
	Release(ctx context.Context, trackID domain.TrackId, fence Fence, availableAt time.Time) error
	Settle(ctx context.Context, trackID domain.TrackId, fence Fence) error
}

type QueueDepthReader interface {
	PendingDepth(ctx context.Context) (pending int, oldestAge time.Duration, err error)
}

type JobNotifier interface {
	Listen(ctx context.Context, wake chan<- struct{})
}
