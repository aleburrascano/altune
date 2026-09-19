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

// admissionError carries a stable error code and HTTP status so admission
// sentinels route through httputil.HandleServiceError like other handlers. The
// literal statuses avoid importing net/http, which the application layer forbids.
type admissionError struct {
	msg    string
	status int
	code   string
}

func (e *admissionError) Error() string     { return e.msg }
func (e *admissionError) HTTPStatus() int   { return e.status }
func (e *admissionError) ErrorCode() string { return e.code }

var (
	ErrRetryNotFailed = &admissionError{
		msg:    "track is not in failed state",
		status: 409,
		code:   "acquisition.retry_not_failed",
	}
	ErrCooldownActive = &admissionError{
		msg:    "cooldown active, try again later",
		status: 429,
		code:   "acquisition.cooldown_active",
	}
	ErrReacquireNotReady = &admissionError{
		msg:    "track has no audio to replace",
		status: 409,
		code:   "acquisition.reacquire_not_ready",
	}
)

// cooldownRefusal is ErrCooldownActive carrying a wait for the client, which
// otherwise has to assume one. Reserve does not report the time left, so the
// whole window is carried: an upper bound, so a client that honors it is
// admitted. It unwraps to the sentinel, which callers still match on.
type cooldownRefusal struct {
	window time.Duration
}

func (e cooldownRefusal) Error() string             { return ErrCooldownActive.Error() }
func (e cooldownRefusal) Unwrap() error             { return ErrCooldownActive }
func (e cooldownRefusal) RetryAfter() time.Duration { return e.window }

// releaseTimeout bounds the refund of a reservation whose job was not queued.
const releaseTimeout = 5 * time.Second

// cooldownGate enforces one admission per track per window for one kind. The
// window lives in a ports.CooldownStore shared by every process, so a restart
// or a second replica cannot reopen it.
type cooldownGate struct {
	store    ports.CooldownStore
	kind     ports.CooldownKind
	cooldown time.Duration
}

// run reserves the cooldown for the track, calls schedule, and releases the
// reservation when schedule reports the job was not queued. Reserving before
// scheduling keeps concurrent requests for the same track from both passing.
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

// release refunds a reservation so a refused schedule does not burn the
// cooldown. It outlives a cancelled request; a failure only leaves the cooldown
// consumed, so it is logged rather than returned.
func (g cooldownGate) release(ctx context.Context, trackID domain.TrackId, at time.Time) {
	relCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
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

// Admit checks the track may be retried and, if so, calls schedule. The retry
// cooldown stays consumed only when schedule returns nil (the job was queued);
// a schedule error is returned as-is and leaves the cooldown untouched. A store
// error is returned wrapped and schedule is not called.
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

// Admit checks the track may be reacquired and, if so, calls schedule. The
// cooldown stays consumed only when schedule returns nil (the job was queued).
func (a *ReacquireAdmission) Admit(ctx context.Context, track *domain.Track, schedule func() error) error {
	if !track.IsStreamable() {
		return ErrReacquireNotReady
	}
	return a.gate.run(ctx, track.ID, schedule)
}
