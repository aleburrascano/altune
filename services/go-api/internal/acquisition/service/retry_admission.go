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
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()
	if last, ok := g.lastAt[key]; ok && now.Sub(last) < g.cooldown {
		return false
	}
	g.lastAt[key] = now
	for k, v := range g.lastAt {
		if now.Sub(v) >= 2*g.cooldown {
			delete(g.lastAt, k)
		}
	}
	return true
}

type RetryAdmission struct {
	gate *cooldownGate
}

func NewRetryAdmission() *RetryAdmission {
	return &RetryAdmission{gate: newCooldownGate(RetryCooldown)}
}

func (a *RetryAdmission) Admit(track *domain.Track) error {
	if track.AcquisitionStatus != domain.AcquisitionFailed {
		return ErrRetryNotFailed
	}
	if !a.gate.admit(track.ID.String()) {
		return ErrCooldownActive
	}
	return nil
}

type ReacquireAdmission struct {
	gate *cooldownGate
}

func NewReacquireAdmission() *ReacquireAdmission {
	return &ReacquireAdmission{gate: newCooldownGate(ReacquireCooldown)}
}

func (a *ReacquireAdmission) Admit(track *domain.Track) error {
	if !track.IsStreamable() {
		return ErrReacquireNotReady
	}
	if !a.gate.admit(track.ID.String()) {
		return ErrCooldownActive
	}
	return nil
}
