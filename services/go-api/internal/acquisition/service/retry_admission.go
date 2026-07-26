package service

import (
	"errors"
	"sync"
	"time"

	"altune/go-api/internal/catalog/domain"
)

const (
	RetryCooldown     = 60 * time.Second
	ReacquireCooldown = 60 * time.Second
)

var (
	ErrRetryNotFailed    = errors.New("track is not in failed state")
	ErrRetryCooldown     = errors.New("retry cooldown active")
	ErrReacquireNotReady = errors.New("track has no audio to replace")
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
		return ErrRetryCooldown
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
		return ErrRetryCooldown
	}
	return nil
}
