package ports

import (
	"altune/go-api/internal/catalog/domain"
	"context"
	"time"
)

type CooldownKind string

const (
	CooldownRetry     CooldownKind = "retry"
	CooldownReacquire CooldownKind = "reacquire"
)

type CooldownStore interface {
	Reserve(ctx context.Context, trackID domain.TrackId, kind CooldownKind, cooldown time.Duration) (at time.Time, ok bool, err error)
	Release(ctx context.Context, trackID domain.TrackId, kind CooldownKind, at time.Time) error
}
