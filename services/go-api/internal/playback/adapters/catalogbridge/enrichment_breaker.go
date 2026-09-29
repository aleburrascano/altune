package catalogbridge

import (
	"altune/go-api/internal/playback/ports"
	"log/slog"
	"sync"
	"time"
)

type breakerState int

const (
	breakerClosed breakerState = iota
	breakerOpen
	breakerHalfOpen
)

const (
	enrichmentFailureThreshold = 5
	enrichmentOpenDuration     = 30 * time.Second
)

type enrichmentBreaker struct {
	mu           sync.Mutex
	state        breakerState
	failures     int
	lastFailedAt time.Time
	probing      bool
	generation   uint64
	now          func() time.Time
	metrics      ports.EnrichmentMetrics
}

func newEnrichmentBreaker(metrics ports.EnrichmentMetrics) *enrichmentBreaker {
	return &enrichmentBreaker{now: time.Now, metrics: metrics}
}

type admission struct {
	generation uint64
	probe      bool
}

func (b *enrichmentBreaker) allow() (bool, admission) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case breakerOpen:
		if b.probing || !b.openWindowElapsed() {
			return false, admission{}
		}
		b.state = breakerHalfOpen
		slog.Warn("now-playing enrichment breaker half-open (probing catalog recovery)")
		return true, b.admitProbe()
	case breakerHalfOpen:
		if b.probing {
			return false, admission{}
		}
		return true, b.admitProbe()
	default:
		return true, admission{generation: b.generation}
	}
}

func (b *enrichmentBreaker) openWindowElapsed() bool {
	return b.now().Sub(b.lastFailedAt) > enrichmentOpenDuration
}

func (b *enrichmentBreaker) admitProbe() admission {
	b.probing = true
	return admission{generation: b.generation, probe: true}
}

func (b *enrichmentBreaker) release(adm admission) {
	if !adm.probe {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	b.probing = false
}

func (b *enrichmentBreaker) recordSuccess(adm admission) {
	if !b.closeCircuit(adm) {
		return
	}
	slog.Info("now-playing enrichment breaker closed (catalog recovered)")
}

func (b *enrichmentBreaker) closeCircuit(adm admission) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if adm.probe {
		return b.closeAfterProbe()
	}
	if b.state == breakerClosed && adm.generation == b.generation {
		b.failures = 0
	}
	return false
}

func (b *enrichmentBreaker) closeAfterProbe() bool {
	if b.state != breakerHalfOpen {
		return false
	}
	b.state = breakerClosed
	b.failures = 0
	b.metrics.EnrichmentBreakerClosed()
	return true
}

func (b *enrichmentBreaker) recordFailure() {
	failures, tripped := b.countFailure()
	if !tripped {
		return
	}
	slog.Warn("now-playing enrichment breaker opened (catalog failing)", "failures", failures)
}

func (b *enrichmentBreaker) countFailure() (failures int, tripped bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.failures++
	b.lastFailedAt = b.now()

	if b.state == breakerOpen || !b.shouldTrip() {
		return b.failures, false
	}
	b.state = breakerOpen
	b.generation++
	b.metrics.EnrichmentBreakerOpened()
	return b.failures, true
}

func (b *enrichmentBreaker) shouldTrip() bool {
	return b.state == breakerHalfOpen || b.failures >= enrichmentFailureThreshold
}
