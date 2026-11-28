package service

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/resilience"
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

type (
	CircuitState = resilience.CircuitState
	Admission    = resilience.CircuitAdmission
)

const (
	CircuitClosed   = resilience.CircuitClosed
	CircuitOpen     = resilience.CircuitOpen
	CircuitHalfOpen = resilience.CircuitHalfOpen
)

const (
	FailureThreshold = 5
	OpenDuration     = 30 * time.Second
	probeLease       = 30 * time.Second
)

type CircuitBreaker struct {
	mu       sync.Mutex
	now      clock.Func
	skew     atomic.Int64
	circuits map[domain.ProviderName]*resilience.CircuitBreaker
}

func NewCircuitBreaker(now clock.Func) *CircuitBreaker {
	if now == nil {
		panic("providerguard.NewCircuitBreaker: a clock is required")
	}
	return &CircuitBreaker{
		now:      now,
		circuits: make(map[domain.ProviderName]*resilience.CircuitBreaker),
	}
}

func (cb *CircuitBreaker) AllowRequest(ctx context.Context, provider domain.ProviderName) (Admission, bool) {
	return cb.circuit(provider).Allow(ctx)
}

func (cb *CircuitBreaker) RecordSuccess(ctx context.Context, provider domain.ProviderName, adm Admission) {
	cb.circuit(provider).Success(ctx, adm)
}

func (cb *CircuitBreaker) RecordFailure(ctx context.Context, provider domain.ProviderName, adm Admission) {
	cb.circuit(provider).Failure(ctx, adm)
}

func (cb *CircuitBreaker) ReleaseProbe(provider domain.ProviderName, adm Admission) {
	cb.circuit(provider).Release(adm)
}

func (cb *CircuitBreaker) GetStatus(provider domain.ProviderName) domain.ProviderStatus {
	if cb.circuit(provider).State() == resilience.CircuitOpen {
		return domain.ProviderStatusCircuitOpen
	}
	return domain.ProviderStatusOK
}

func (cb *CircuitBreaker) circuit(provider domain.ProviderName) *resilience.CircuitBreaker {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	b, ok := cb.circuits[provider]
	if !ok {
		b = resilience.MustNewCircuitBreaker(resilience.CircuitBreakerConfig{
			Threshold:  FailureThreshold,
			OpenFor:    OpenDuration,
			ProbeLease: probeLease,
			Now:        cb.clock,
			OnChange:   logChange(provider),
		})
		cb.circuits[provider] = b
	}
	return b
}

func (cb *CircuitBreaker) clock() time.Time {
	return cb.now().Add(time.Duration(cb.skew.Load()))
}

func logChange(provider domain.ProviderName) func(context.Context, resilience.CircuitChange) {
	return func(ctx context.Context, c resilience.CircuitChange) {
		switch c.Kind {
		case resilience.CircuitTripped:
			slog.WarnContext(ctx, "circuit breaker opened (provider failing)", "provider", provider.String(), "failures", c.Failures)
		case resilience.CircuitProbing:
			slog.WarnContext(ctx, "circuit breaker half-open (probing recovery)", "provider", provider.String())
		case resilience.ProbeLeaseExpired:
			slog.WarnContext(ctx, "circuit breaker probe lease expired (re-probing)", "provider", provider.String())
		case resilience.CircuitRecovered:
			slog.InfoContext(ctx, "circuit breaker closed (provider recovered)", "provider", provider.String())
		}
	}
}
