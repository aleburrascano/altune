package catalogbridge

import (
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
}

func newEnrichmentBreaker() *enrichmentBreaker {
	return &enrichmentBreaker{now: time.Now}
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

// recordSuccess closes the breaker and clears the failure run.
func (b *enrichmentBreaker) recordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state != breakerClosed {
		slog.Info("now-playing enrichment breaker closed (catalog recovered)")
	}
	b.state = breakerClosed
	b.failures = 0
}

// recordFailure counts a failed call and trips the breaker open once the
// failure run crosses the threshold, or immediately if a half-open probe fails.
func (b *enrichmentBreaker) recordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.failures++
	b.lastFailedAt = b.now()

	if b.state != breakerOpen && (b.state == breakerHalfOpen || b.failures >= enrichmentFailureThreshold) {
		b.state = breakerOpen
		slog.Warn("now-playing enrichment breaker opened (catalog failing)", "failures", b.failures)
	}
}
