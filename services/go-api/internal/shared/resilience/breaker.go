package resilience

import (
	"altune/go-api/internal/shared/clock"
	"context"
	"errors"
	"sync"
	"time"
)

type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

type kind int

const (
	CircuitTripped kind = iota + 1
	CircuitProbing
	ProbeLeaseExpired
	CircuitRecovered
)

type CircuitChange struct {
	Kind     kind
	Failures int
}

type CircuitBreakerConfig struct {
	Threshold  int
	OpenFor    time.Duration
	ProbeLease time.Duration
	Now        clock.Func
	OnChange   func(ctx context.Context, c CircuitChange)
}

type CircuitAdmission struct {
	generation uint64
	probe      bool
	probeSeq   uint64
}

type CircuitSnapshot struct {
	State    CircuitState
	Failures int
	Probing  bool
}

type CircuitBreaker struct {
	mu             sync.Mutex
	cfg            CircuitBreakerConfig
	state          CircuitState
	failures       int
	lastFailedAt   time.Time
	generation     uint64
	probing        bool
	probeStartedAt time.Time
	probeSeq       uint64
}

var errClockRequired = errors.New("breaker: the breaker requires a clock")

func newBreaker(cfg CircuitBreakerConfig) (*CircuitBreaker, error) {
	if cfg.Now == nil {
		return nil, errClockRequired
	}
	cfg.Threshold = max(cfg.Threshold, 1)
	return &CircuitBreaker{cfg: cfg}, nil
}

func MustNewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	b, err := newBreaker(cfg)
	if err != nil {
		panic(err)
	}
	return b
}

func (b *CircuitBreaker) Allow(ctx context.Context) (CircuitAdmission, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case CircuitOpen:
		if b.cfg.Now().Sub(b.lastFailedAt) <= b.cfg.OpenFor {
			return CircuitAdmission{}, false
		}
		b.state = CircuitHalfOpen
		b.notify(ctx, CircuitChange{Kind: CircuitProbing})
		return b.admitProbe(), true
	case CircuitHalfOpen:
		if b.probing {
			if b.cfg.Now().Sub(b.probeStartedAt) <= b.cfg.ProbeLease {
				return CircuitAdmission{}, false
			}
			b.notify(ctx, CircuitChange{Kind: ProbeLeaseExpired})
		}
		return b.admitProbe(), true
	case CircuitClosed:
		return CircuitAdmission{generation: b.generation}, true
	}
	return CircuitAdmission{generation: b.generation}, true
}

func (b *CircuitBreaker) Success(ctx context.Context, adm CircuitAdmission) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if adm.probe {
		if b.state != CircuitHalfOpen || adm.probeSeq != b.probeSeq {
			return
		}
		b.state = CircuitClosed
		b.failures = 0
		b.probing = false
		b.notify(ctx, CircuitChange{Kind: CircuitRecovered})
		return
	}
	if b.state == CircuitClosed && adm.generation == b.generation {
		b.failures = 0
	}
}

func (b *CircuitBreaker) Failure(ctx context.Context, adm CircuitAdmission) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.failures++
	b.lastFailedAt = b.cfg.Now()
	if adm.probe && adm.probeSeq == b.probeSeq {
		b.probing = false
	}
	if b.state == CircuitOpen || (b.state != CircuitHalfOpen && b.failures < b.cfg.Threshold) {
		return
	}
	b.state = CircuitOpen
	b.generation++
	b.notify(ctx, CircuitChange{Kind: CircuitTripped, Failures: b.failures})
}

func (b *CircuitBreaker) Release(adm CircuitAdmission) {
	if !adm.probe {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	if adm.probeSeq == b.probeSeq {
		b.probing = false
	}
}

func (b *CircuitBreaker) State() CircuitState {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.state
}

func (b *CircuitBreaker) Snapshot() CircuitSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()

	return CircuitSnapshot{State: b.state, Failures: b.failures, Probing: b.probing}
}

func (b *CircuitBreaker) admitProbe() CircuitAdmission {
	b.probing = true
	b.probeStartedAt = b.cfg.Now()
	b.probeSeq++
	return CircuitAdmission{generation: b.generation, probe: true, probeSeq: b.probeSeq}
}

func (b *CircuitBreaker) notify(ctx context.Context, c CircuitChange) {
	if b.cfg.OnChange != nil {
		b.cfg.OnChange(ctx, c)
	}
}
