package ports

import (
	shared "altune/go-api/internal/shared/kernel"
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
