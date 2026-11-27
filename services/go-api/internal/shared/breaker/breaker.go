package breaker

import (
	"altune/go-api/internal/shared/clock"
	"context"
	"errors"
	"sync"
	"time"
)

type State int

const (
	Closed State = iota
	Open
	HalfOpen
)

type kind int

const (
	Tripped kind = iota + 1
	Probing
	LeaseExpired
	Recovered
)

type Change struct {
	Kind     kind
	Failures int
}

type Config struct {
	Threshold  int
	OpenFor    time.Duration
	ProbeLease time.Duration
	Now        clock.Func
	OnChange   func(ctx context.Context, c Change)
}

type Admission struct {
	generation uint64
	probe      bool
	probeSeq   uint64
}

type Snapshot struct {
	State    State
	Failures int
	Probing  bool
}

type Breaker struct {
	mu             sync.Mutex
	cfg            Config
	state          State
	failures       int
	lastFailedAt   time.Time
	generation     uint64
	probing        bool
	probeStartedAt time.Time
	probeSeq       uint64
}

var errClockRequired = errors.New("breaker: the breaker requires a clock")

func newBreaker(cfg Config) (*Breaker, error) {
	if cfg.Now == nil {
		return nil, errClockRequired
	}
	cfg.Threshold = max(cfg.Threshold, 1)
	return &Breaker{cfg: cfg}, nil
}

func MustNew(cfg Config) *Breaker {
	b, err := newBreaker(cfg)
	if err != nil {
		panic(err)
	}
	return b
}

func (b *Breaker) Allow(ctx context.Context) (Admission, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case Open:
		if b.cfg.Now().Sub(b.lastFailedAt) <= b.cfg.OpenFor {
			return Admission{}, false
		}
		b.state = HalfOpen
		b.notify(ctx, Change{Kind: Probing})
		return b.admitProbe(), true
	case HalfOpen:
		if b.probing {
			if b.cfg.Now().Sub(b.probeStartedAt) <= b.cfg.ProbeLease {
				return Admission{}, false
			}
			b.notify(ctx, Change{Kind: LeaseExpired})
		}
		return b.admitProbe(), true
	default:
		return Admission{generation: b.generation}, true
	}
}

func (b *Breaker) Success(ctx context.Context, adm Admission) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if adm.probe {
		if b.state != HalfOpen || adm.probeSeq != b.probeSeq {
			return
		}
		b.state = Closed
		b.failures = 0
		b.probing = false
		b.notify(ctx, Change{Kind: Recovered})
		return
	}
	if b.state == Closed && adm.generation == b.generation {
		b.failures = 0
	}
}

func (b *Breaker) Failure(ctx context.Context, adm Admission) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.failures++
	b.lastFailedAt = b.cfg.Now()
	if adm.probe && adm.probeSeq == b.probeSeq {
		b.probing = false
	}
	if b.state == Open || (b.state != HalfOpen && b.failures < b.cfg.Threshold) {
		return
	}
	b.state = Open
	b.generation++
	b.notify(ctx, Change{Kind: Tripped, Failures: b.failures})
}

func (b *Breaker) Release(adm Admission) {
	if !adm.probe {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	if adm.probeSeq == b.probeSeq {
		b.probing = false
	}
}

func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.state
}

func (b *Breaker) Snapshot() Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()

	return Snapshot{State: b.state, Failures: b.failures, Probing: b.probing}
}

func (b *Breaker) admitProbe() Admission {
	b.probing = true
	b.probeStartedAt = b.cfg.Now()
	b.probeSeq++
	return Admission{generation: b.generation, probe: true, probeSeq: b.probeSeq}
}

func (b *Breaker) notify(ctx context.Context, c Change) {
	if b.cfg.OnChange != nil {
		b.cfg.OnChange(ctx, c)
	}
}
