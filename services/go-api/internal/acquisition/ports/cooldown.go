package ports

import (
	"altune/go-api/internal/shared"
	"context"
	"time"
)

type CooldownKind string

const (
	CooldownRetry     CooldownKind = "retry"
	CooldownReacquire CooldownKind = "reacquire"
)

type CooldownStore interface {
	Reserve(ctx context.Context, trackID shared.TrackId, kind CooldownKind, cooldown time.Duration) (at time.Time, ok bool, wait time.Duration, err error)
	Release(ctx context.Context, trackID shared.TrackId, kind CooldownKind, at time.Time) error
}
