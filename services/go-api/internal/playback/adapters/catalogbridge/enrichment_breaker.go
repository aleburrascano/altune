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
	now          func() time.Time
	metrics      ports.EnrichmentMetrics
}

func newEnrichmentBreaker(metrics ports.EnrichmentMetrics) *enrichmentBreaker {
	return &enrichmentBreaker{now: time.Now, metrics: metrics}
}

func noProbeHeld() {}

func (b *enrichmentBreaker) allow() (bool, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case breakerOpen:
		if b.probing || !b.openWindowElapsed() {
			return false, noProbeHeld
		}
		b.state = breakerHalfOpen
		slog.Warn("now-playing enrichment breaker half-open (probing catalog recovery)")
		return true, b.holdProbe()
	case breakerHalfOpen:
		if b.probing {
			return false, noProbeHeld
		}
		return true, b.holdProbe()
	default:
		return true, noProbeHeld
	}
}

func (b *enrichmentBreaker) openWindowElapsed() bool {
	return b.now().Sub(b.lastFailedAt) > enrichmentOpenDuration
}

func (b *enrichmentBreaker) holdProbe() func() {
	b.probing = true
	return b.releaseProbe
}

func (b *enrichmentBreaker) releaseProbe() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.probing = false
}

func (b *enrichmentBreaker) recordSuccess() {
	if !b.closeCircuit() {
		return
	}
	slog.Info("now-playing enrichment breaker closed (catalog recovered)")
}

func (b *enrichmentBreaker) closeCircuit() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	recovered := b.state != breakerClosed
	b.state = breakerClosed
	b.failures = 0
	if recovered {
		b.metrics.EnrichmentBreakerClosed()
	}
	return recovered
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
	b.metrics.EnrichmentBreakerOpened()
	return b.failures, true
}

func (b *enrichmentBreaker) shouldTrip() bool {
	return b.state == breakerHalfOpen || b.failures >= enrichmentFailureThreshold
}
