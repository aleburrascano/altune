package service

import (
	"errors"
	"sync"
	"time"

	"altune/go-api/internal/catalog/domain"
)

const RetryCooldown = 60 * time.Second

var (
	ErrRetryNotFailed = errors.New("track is not in failed state")
	ErrRetryCooldown  = errors.New("retry cooldown active")
)

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
