package catalogbridge

import (
	"altune/go-api/internal/playback/ports"
	"log/slog"
	"sync"
	"time"
)

// enrichmentBreaker is a single-circuit fast-fail guard around the now-playing
// catalog enrichment call. Without it a sustained catalog outage makes every
// resume request pay the full per-call timeout before degrading; once a run of
// failures trips the breaker, further calls short-circuit until the catalog is
// given a chance to recover. It is scoped to this one dependency on purpose —
// the repo's general breaker lives in the discovery module and is keyed by
// provider, which does not fit a single enrichment lookup.
type breakerState int

const (
	breakerClosed breakerState = iota
	breakerOpen
	breakerHalfOpen
)

const (
	// enrichmentFailureThreshold is the run of consecutive-ish failures that
	// trips the breaker open.
	enrichmentFailureThreshold = 5
	// enrichmentOpenDuration is how long the breaker stays open before it
	// admits a single probe to test recovery.
	enrichmentOpenDuration = 30 * time.Second
)

type enrichmentBreaker struct {
	mu           sync.Mutex
	state        breakerState
	failures     int
	lastFailedAt time.Time
	// probing is owned by the admitted probe's release alone, never by the
	// outcome it records, so a probe that never reaches a verdict still frees it.
	probing bool
	now     func() time.Time
	// metrics receives the state transitions, which only the breaker can see.
	// The logs record that a transition happened; an operator needs to read
	// whether it still holds. Its edges are written while b.mu is held, so a
	// sink must stay cheap and must never call back into the breaker.
	metrics ports.EnrichmentMetrics
}

func newEnrichmentBreaker(metrics ports.EnrichmentMetrics) *enrichmentBreaker {
	return &enrichmentBreaker{now: time.Now, metrics: metrics}
}

// noProbeHeld releases nothing: the call was admitted in the closed state and
// holds no recovery-probe slot.
func noProbeHeld() {}

// allow reports whether a call may proceed, and returns the release its caller
// must run on every return path. A call admitted while open or half-open holds
// the single recovery-probe slot until it releases; without that release a
// caller who vanishes before the probe reaches a verdict would hold the slot for
// the life of the process, and no later call could ever probe again.
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

// holdProbe claims the single probe slot and returns its release. The caller
// holds b.mu. The slot's holder is the only call that can free it — every other
// call is turned away while it is held — so a release can never free a probe it
// does not own.
func (b *enrichmentBreaker) holdProbe() func() {
	b.probing = true
	return b.releaseProbe
}

func (b *enrichmentBreaker) releaseProbe() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.probing = false
}

// recordSuccess closes the breaker; only the call that ended a degraded run
// announces the recovery.
func (b *enrichmentBreaker) recordSuccess() {
	if !b.closeCircuit() {
		return
	}
	slog.Info("now-playing enrichment breaker closed (catalog recovered)")
}

// closeCircuit clears the failure run and reports whether this call is the one
// that brought the breaker back out of a degraded state. The gauge edge is
// written here, under the lock that decides the transition: written after the
// unlock it can land out of order with a concurrent trip, and since an
// already-open breaker never re-announces itself, a stale healthy edge would
// stick for the rest of the outage.
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

// recordFailure counts a failed call; only the call that tripped the breaker
// announces the outage, so a sustained one is reported once, not per failure.
func (b *enrichmentBreaker) recordFailure() {
	failures, tripped := b.countFailure()
	if !tripped {
		return
	}
	slog.Warn("now-playing enrichment breaker opened (catalog failing)", "failures", failures)
}

// countFailure adds one failure to the run and reports it alongside whether
// this call is the one that tripped the breaker open. The gauge edge is written
// here for the same reason as in closeCircuit: only under the lock do the edges
// reach the gauge in the order the transitions happened.
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

// shouldTrip reports whether the breaker has seen enough: a failed recovery
// probe re-opens at once, a closed breaker only after the full failure run.
// The caller holds b.mu.
func (b *enrichmentBreaker) shouldTrip() bool {
	return b.state == breakerHalfOpen || b.failures >= enrichmentFailureThreshold
}
