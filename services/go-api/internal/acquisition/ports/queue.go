package ports

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"time"
)

type JobKind string

const (
	JobKindAcquire JobKind = "acquire"
	JobKindReplace JobKind = "replace"
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
	Enqueue(ctx context.Context, trackID domain.TrackId, kind JobKind, delay time.Duration) error
	Claim(ctx context.Context, lease time.Duration) (Job, error)
	Heartbeat(ctx context.Context, trackID domain.TrackId, fence Fence, lease time.Duration) error
	Release(ctx context.Context, trackID domain.TrackId, fence Fence, delay time.Duration) error
	Settle(ctx context.Context, trackID domain.TrackId, fence Fence) error
}

type QueueDepthReader interface {
	PendingDepth(ctx context.Context) (pending int, oldestAge time.Duration, err error)
}

type JobNotifier interface {
	Listen(ctx context.Context, wake chan<- struct{})
}
