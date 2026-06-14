package service

import (
	"sync"
	"time"

	"altune/go-api/internal/discovery/domain"
)

type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

const (
	failureThreshold = 5
	openDuration     = 30 * time.Second
)

type circuitEntry struct {
	state        CircuitState
	failures     int
	lastFailedAt time.Time
}

type CircuitBreaker struct {
	mu       sync.Mutex
	circuits map[domain.ProviderName]*circuitEntry
}

func NewCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{
		circuits: make(map[domain.ProviderName]*circuitEntry),
	}
}

func (cb *CircuitBreaker) AllowRequest(provider domain.ProviderName) bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	entry := cb.getOrCreate(provider)

	switch entry.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		if time.Since(entry.lastFailedAt) > openDuration {
			entry.state = CircuitHalfOpen
			return true
		}
		return false
	case CircuitHalfOpen:
		return true
	}
	return true
}

func (cb *CircuitBreaker) RecordSuccess(provider domain.ProviderName) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	entry := cb.getOrCreate(provider)
	entry.state = CircuitClosed
	entry.failures = 0
}

func (cb *CircuitBreaker) RecordFailure(provider domain.ProviderName) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	entry := cb.getOrCreate(provider)
	entry.failures++
	entry.lastFailedAt = time.Now()

	if entry.failures >= failureThreshold {
		entry.state = CircuitOpen
	}
}

func (cb *CircuitBreaker) GetStatus(provider domain.ProviderName) domain.ProviderStatus {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	entry := cb.getOrCreate(provider)
	switch entry.state {
	case CircuitOpen:
		return domain.ProviderStatusCircuitOpen
	default:
		return domain.ProviderStatusOK
	}
}

func (cb *CircuitBreaker) getOrCreate(provider domain.ProviderName) *circuitEntry {
	entry, ok := cb.circuits[provider]
	if !ok {
		entry = &circuitEntry{state: CircuitClosed}
		cb.circuits[provider] = entry
	}
	return entry
}
