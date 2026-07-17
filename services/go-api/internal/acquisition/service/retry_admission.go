package service

import (
	"errors"
	"sync"
	"time"

	"altune/go-api/internal/catalog/domain"
)

// RetryCooldown is the minimum interval between manual re-acquisition retries
// for a single track.
const RetryCooldown = 60 * time.Second

// Retry admission outcomes, mapped to transport codes by the handler.
var (
	// ErrRetryNotFailed rejects retries for tracks not in AcquisitionFailed state.
	ErrRetryNotFailed = errors.New("track is not in failed state")
	// ErrRetryCooldown rejects retries admitted less than RetryCooldown ago.
	ErrRetryCooldown = errors.New("retry cooldown active")
)

// RetryAdmission decides whether a manual re-acquisition retry may run: only
// tracks in AcquisitionFailed state, at most one per track per cooldown window.
// It holds the whole admission policy — the failed-state check previously lived
// in the HTTP handler — so a second retry entry point cannot skip half of it.
type RetryAdmission struct {
	mu        sync.Mutex
	cooldown  time.Duration
	lastRetry map[string]time.Time
}

func NewRetryAdmission() *RetryAdmission {
	return &RetryAdmission{
		cooldown:  RetryCooldown,
		lastRetry: make(map[string]time.Time),
	}
}

// Admit reports whether a retry for track may proceed now. A nil return records
// the admission; otherwise it returns ErrRetryNotFailed or ErrRetryCooldown.
// Stale entries are pruned opportunistically on each call.
func (a *RetryAdmission) Admit(track *domain.Track) error {
	if track.AcquisitionStatus != domain.AcquisitionFailed {
		return ErrRetryNotFailed
	}
	key := track.ID.String()

	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()
	if last, ok := a.lastRetry[key]; ok && now.Sub(last) < a.cooldown {
		return ErrRetryCooldown
	}
	a.lastRetry[key] = now
	for k, v := range a.lastRetry {
		if now.Sub(v) >= 2*a.cooldown {
			delete(a.lastRetry, k)
		}
	}
	return nil
}
