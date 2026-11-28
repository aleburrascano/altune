package service

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/resilience"
	"time"
)

type CircuitSnapshot = resilience.CircuitSnapshot

func (cb *CircuitBreaker) Snapshot(provider domain.ProviderName) CircuitSnapshot {
	return cb.circuit(provider).Snapshot()
}

func (cb *CircuitBreaker) AdvanceClock(d time.Duration) {
	cb.skew.Add(int64(d))
}
