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
	// probeLease bounds how long a half-open probe may hold the single probe
	// slot without being resolved by RecordSuccess, RecordFailure or
	// ReleaseProbe. It is a backstop for a probe abandoned with no outcome
	// (e.g. a provider that ignores its context and never returns), and sits
	// well above every provider's search timeout so a live probe is never
	// preempted.
	probeLease = 30 * time.Second
)

type circuitEntry struct {
	state        CircuitState
	failures     int
	lastFailedAt time.Time
	probing      bool
	// probeStartedAt is when the in-flight half-open probe was admitted; only
	// meaningful while probing is true.
	probeStartedAt time.Time
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
			entry.probing = true
			entry.probeStartedAt = time.Now()
			slog.Warn("circuit breaker half-open (probing recovery)",
				"provider", provider.String())
			return true
		}
		return false
	case CircuitHalfOpen:
		if entry.probing && time.Since(entry.probeStartedAt) <= probeLease {
			return false
		}
		if entry.probing {
			slog.Warn("circuit breaker probe lease expired (re-probing)",
				"provider", provider.String())
		}
		entry.probing = true
		entry.probeStartedAt = time.Now()
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
	entry.lastFailedAt = time.Now()
	entry.probing = false

	if entry.state != CircuitOpen && (entry.state == CircuitHalfOpen || entry.failures >= failureThreshold) {
		entry.state = CircuitOpen
		slog.Warn("circuit breaker opened (provider failing)",
			"provider", provider.String(), "failures", entry.failures)
	}
}

// ReleaseProbe hands back an admitted half-open probe slot without recording
// an outcome, for a call abandoned for reasons that say nothing about the
// provider's health (the caller's context was canceled). The circuit stays
// half-open so the next request is admitted as a fresh probe. It is a no-op in
// any other state.
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
