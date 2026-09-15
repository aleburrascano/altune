package service

import (
	"sync"
	"time"

	"altune/go-api/internal/catalog/domain"
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

type cooldownGate struct {
	mu       sync.Mutex
	cooldown time.Duration
	lastAt   map[string]time.Time
}

func newCooldownGate(cooldown time.Duration) *cooldownGate {
	return &cooldownGate{cooldown: cooldown, lastAt: make(map[string]time.Time)}
}

func (g *cooldownGate) admit(key string) bool {
	_, ok := g.reserve(key)
	return ok
}

// reserve atomically checks the cooldown and records now as the key's last
// admission, returning the recorded time so release can undo exactly it.
func (g *cooldownGate) reserve(key string) (time.Time, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()
	if last, ok := g.lastAt[key]; ok && now.Sub(last) < g.cooldown {
		return time.Time{}, false
	}
	g.lastAt[key] = now
	for k, v := range g.lastAt {
		if now.Sub(v) >= 2*g.cooldown {
			delete(g.lastAt, k)
		}
	}
	return now, true
}

// release undoes a reservation whose job was never queued, so a refused
// schedule does not burn the cooldown. A newer reservation is left untouched.
func (g *cooldownGate) release(key string, at time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if last, ok := g.lastAt[key]; ok && last.Equal(at) {
		delete(g.lastAt, key)
	}
}

// run reserves the cooldown for key, calls schedule, and releases the
// reservation when schedule reports the job was not queued. Reserving before
// scheduling keeps concurrent requests for the same key from both passing.
func (g *cooldownGate) run(key string, schedule func() error) error {
	at, ok := g.reserve(key)
	if !ok {
		return ErrCooldownActive
	}
	if err := schedule(); err != nil {
		g.release(key, at)
		return err
	}
	return nil
}

type RetryAdmission struct {
	gate *cooldownGate
}

func NewRetryAdmission() *RetryAdmission {
	return &RetryAdmission{gate: newCooldownGate(RetryCooldown)}
}

// Admit checks the track may be retried and, if so, calls schedule. The retry
// cooldown stays consumed only when schedule returns nil (the job was queued);
// a schedule error is returned as-is and leaves the cooldown untouched.
func (a *RetryAdmission) Admit(track *domain.Track, schedule func() error) error {
	if track.AcquisitionStatus != domain.AcquisitionFailed {
		return ErrRetryNotFailed
	}
	return a.gate.run(track.ID.String(), schedule)
}

type ReacquireAdmission struct {
	gate *cooldownGate
}

func NewReacquireAdmission() *ReacquireAdmission {
	return &ReacquireAdmission{gate: newCooldownGate(ReacquireCooldown)}
}

// Admit checks the track may be reacquired and, if so, calls schedule. The
// cooldown stays consumed only when schedule returns nil (the job was queued).
func (a *ReacquireAdmission) Admit(track *domain.Track, schedule func() error) error {
	if !track.IsStreamable() {
		return ErrReacquireNotReady
	}
	return a.gate.run(track.ID.String(), schedule)
}
