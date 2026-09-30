package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"context"
	"fmt"
	"log/slog"
	"time"
)

const (
	RetryCooldown     = 60 * time.Second
	ReacquireCooldown = 60 * time.Second
)

type cooldownRefusal struct {
	window time.Duration
}

func (e cooldownRefusal) Error() string             { return ErrCooldownActive.Error() }
func (e cooldownRefusal) Unwrap() error             { return ErrCooldownActive }
func (e cooldownRefusal) RetryAfter() time.Duration { return e.window }

const releaseTimeout = 5 * time.Second

type cooldownGate struct {
	store    ports.CooldownStore
	kind     ports.CooldownKind
	cooldown time.Duration
}

func (g cooldownGate) run(ctx context.Context, trackID domain.TrackId, schedule func() error) error {
	at, ok, err := g.store.Reserve(ctx, trackID, g.kind, g.cooldown)
	if err != nil {
		return fmt.Errorf("%s admission: %w", g.kind, err)
	}
	if !ok {
		return cooldownRefusal{window: g.cooldown}
	}
	if err := schedule(); err != nil {
		g.release(ctx, trackID, at)
		return err
	}
	return nil
}

func (g cooldownGate) release(ctx context.Context, trackID domain.TrackId, at time.Time) {
	relCtx, cancel := detachedTimeout(ctx, releaseTimeout)
	defer cancel()
	if err := g.store.Release(relCtx, trackID, g.kind, at); err != nil {
		slog.WarnContext(ctx, "acquisition: cooldown release failed", "kind", string(g.kind), "track_id", trackID.String(), "error", err)
	}
}

type RetryAdmission struct {
	gate cooldownGate
}

func NewRetryAdmission(store ports.CooldownStore) *RetryAdmission {
	return &RetryAdmission{gate: cooldownGate{store: store, kind: ports.CooldownRetry, cooldown: RetryCooldown}}
}

func (a *RetryAdmission) Admit(ctx context.Context, track *domain.Track, schedule func() error) error {
	if track.AcquisitionStatus != domain.AcquisitionFailed {
		return ErrRetryNotFailed
	}
	return a.gate.run(ctx, track.ID, schedule)
}

type ReacquireAdmission struct {
	gate cooldownGate
}

func NewReacquireAdmission(store ports.CooldownStore) *ReacquireAdmission {
	return &ReacquireAdmission{gate: cooldownGate{store: store, kind: ports.CooldownReacquire, cooldown: ReacquireCooldown}}
}

func (a *ReacquireAdmission) Admit(ctx context.Context, track *domain.Track, schedule func() error) error {
	if !track.IsStreamable() {
		return ErrReacquireNotReady
	}
	return a.gate.run(ctx, track.ID, schedule)
}
