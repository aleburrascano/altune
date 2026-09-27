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

var ErrNoJobAvailable = errors.New("no acquisition job available")

type Job struct {
	TrackID  domain.TrackId
	UserID   shared.UserId
	Kind     JobKind
	Attempts int
}

type JobQueue interface {
	Enqueue(ctx context.Context, trackID domain.TrackId, kind JobKind, availableAt time.Time) error
	Claim(ctx context.Context, lease time.Duration) (Job, error)
	Heartbeat(ctx context.Context, trackID domain.TrackId, lease time.Duration) error
	Release(ctx context.Context, trackID domain.TrackId, availableAt time.Time) error
	Settle(ctx context.Context, trackID domain.TrackId) error
}
