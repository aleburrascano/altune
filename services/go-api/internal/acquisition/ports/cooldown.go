package ports

import (
	"altune/go-api/internal/catalog/domain"
	"context"
	"time"
)

// CooldownKind names a manual acquisition action rate limited per track. Each
// kind has its own window, so a retry does not consume the reacquire cooldown.
type CooldownKind string

const (
	CooldownRetry     CooldownKind = "retry"
	CooldownReacquire CooldownKind = "reacquire"
)

// CooldownStore records the last admission of a (track, kind) pair in storage
// shared by every go-api process, so the cooldown holds across restarts and
// replicas rather than per process.
type CooldownStore interface {
	// Reserve atomically claims the cooldown. It returns ok=false when an
	// admission was recorded less than cooldown ago; otherwise it records one
	// and returns its timestamp, which Release takes to undo exactly it.
	Reserve(ctx context.Context, trackID domain.TrackId, kind CooldownKind, cooldown time.Duration) (at time.Time, ok bool, err error)
	// Release deletes the admission recorded at at. A newer admission, recorded
	// by a later Reserve, is left untouched.
	Release(ctx context.Context, trackID domain.TrackId, kind CooldownKind, at time.Time) error
}
