package providercall

import (
	"altune/go-api/internal/discovery/domain"
	"time"
)

type CircuitSnapshot struct {
	State    CircuitState
	Failures int
	Probing  bool
}

func (cb *CircuitBreaker) Snapshot(provider domain.ProviderName) CircuitSnapshot {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	entry := cb.getOrCreate(provider)
	return CircuitSnapshot{State: entry.state, Failures: entry.failures, Probing: entry.probing}
}

func (cb *CircuitBreaker) AdvanceClock(d time.Duration) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	prev := cb.now
	cb.now = func() time.Time { return prev().Add(d) }
}
