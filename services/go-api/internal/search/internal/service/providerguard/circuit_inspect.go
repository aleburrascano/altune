package providerguard

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/breaker"
	"time"
)

type CircuitSnapshot = breaker.Snapshot

func (cb *CircuitBreaker) Snapshot(provider domain.ProviderName) CircuitSnapshot {
	return cb.circuit(provider).Snapshot()
}

func (cb *CircuitBreaker) AdvanceClock(d time.Duration) {
	cb.skew.Add(int64(d))
}
