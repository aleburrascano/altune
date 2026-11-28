package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"fmt"
	"log/slog"
	"time"
)

const (
	RetryCooldown     = 60 * time.Second
	ReacquireCooldown = 60 * time.Second

	minRetryAfter = time.Second
)

type cooldownRefusedError struct {
	wait time.Duration
}

func (e cooldownRefusedError) Error() string             { return domain.ErrCooldownActive.Error() }
func (e cooldownRefusedError) Unwrap() error             { return domain.ErrCooldownActive }
func (e cooldownRefusedError) RetryAfter() time.Duration { return max(e.wait, minRetryAfter) }

const releaseTimeout = 5 * time.Second

type cooldownGate struct {
	store    ports.CooldownStore
	kind     ports.CooldownKind
	cooldown time.Duration
}

func (g cooldownGate) run(ctx context.Context, trackID ids.TrackId, schedule func() error) error {
	at, ok, wait, err := g.store.Reserve(ctx, trackID, g.kind, g.cooldown)
	if err != nil {
		return fmt.Errorf("%s admission: %w", g.kind, err)
	}
	if !ok {
		return cooldownRefusedError{wait: wait}
	}
	if err := schedule(); err != nil {
		g.release(ctx, trackID, at)
		return err
	}
	return nil
}

func (g cooldownGate) release(ctx context.Context, trackID ids.TrackId, at time.Time) {
	relCtx, cancel := background.Detach(ctx, releaseTimeout)
	defer cancel()
	if err := g.store.Release(relCtx, trackID, g.kind, at); err != nil {
		slog.WarnContext(ctx, "acquisition: cooldown release failed", "kind", string(g.kind), logging.KeyTrackID, trackID.String(), logging.KeyError, err)
	}
}

type RetryAdmission struct {
	gate cooldownGate
}

func NewRetryAdmission(store ports.CooldownStore) *RetryAdmission {
	return &RetryAdmission{gate: cooldownGate{store: store, kind: ports.CooldownRetry, cooldown: RetryCooldown}}
}

func (a *RetryAdmission) Admit(ctx context.Context, track *ports.TrackSnapshot, schedule func() error) error {
	if track == nil || track.Status != ports.TrackFailed {
		return domain.ErrRetryNotFailed
	}
	return a.gate.run(ctx, track.ID, schedule)
}

type ReacquireAdmission struct {
	gate cooldownGate
}

func NewReacquireAdmission(store ports.CooldownStore) *ReacquireAdmission {
	return &ReacquireAdmission{gate: cooldownGate{store: store, kind: ports.CooldownReacquire, cooldown: ReacquireCooldown}}
}

func (a *ReacquireAdmission) Admit(ctx context.Context, track *ports.TrackSnapshot, schedule func() error) error {
	if track == nil || !track.Streamable {
		return domain.ErrReacquireNotReady
	}
	return a.gate.run(ctx, track.ID, schedule)
}
