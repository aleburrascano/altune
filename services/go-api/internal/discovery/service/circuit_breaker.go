package service

import (
	"log/slog"
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
	probeLease       = 30 * time.Second
)

type circuitEntry struct {
	state          CircuitState
	failures       int
	lastFailedAt   time.Time
	probing        bool
	probeStartedAt time.Time
}

type CircuitBreaker struct {
	mu       sync.Mutex
	now      func() time.Time
	circuits map[domain.ProviderName]*circuitEntry
}

func NewCircuitBreaker() *CircuitBreaker {
	return newCircuitBreaker(time.Now)
}

func newCircuitBreaker(now func() time.Time) *CircuitBreaker {
	return &CircuitBreaker{
		now:      now,
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
		if cb.now().Sub(entry.lastFailedAt) > openDuration {
			entry.state = CircuitHalfOpen
			entry.probing = true
			entry.probeStartedAt = cb.now()
			slog.Warn("circuit breaker half-open (probing recovery)",
				"provider", provider.String())
			return true
		}
		return false
	case CircuitHalfOpen:
		if entry.probing && cb.now().Sub(entry.probeStartedAt) <= probeLease {
			return false
		}
		if entry.probing {
			slog.Warn("circuit breaker probe lease expired (re-probing)",
				"provider", provider.String())
		}
		entry.probing = true
		entry.probeStartedAt = cb.now()
		return true
	}
	return true
}

func (cb *CircuitBreaker) RecordSuccess(provider domain.ProviderName) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	entry := cb.getOrCreate(provider)
	if entry.state != CircuitClosed {
		slog.Info("circuit breaker closed (provider recovered)",
			"provider", provider.String())
	}
	entry.state = CircuitClosed
	entry.failures = 0
	entry.probing = false
}

func (cb *CircuitBreaker) RecordFailure(provider domain.ProviderName) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	entry := cb.getOrCreate(provider)
	entry.failures++
	entry.lastFailedAt = cb.now()
	entry.probing = false

	if entry.state != CircuitOpen && (entry.state == CircuitHalfOpen || entry.failures >= failureThreshold) {
		entry.state = CircuitOpen
		slog.Warn("circuit breaker opened (provider failing)",
			"provider", provider.String(), "failures", entry.failures)
	}
}

func (cb *CircuitBreaker) ReleaseProbe(provider domain.ProviderName) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	entry := cb.getOrCreate(provider)
	if entry.state == CircuitHalfOpen {
		entry.probing = false
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
