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
	probing      bool
	now          func() time.Time
}

func newEnrichmentBreaker() *enrichmentBreaker {
	return &enrichmentBreaker{now: time.Now}
}

// allow reports whether a call may proceed. When open it short-circuits until
// the open window elapses, then admits one probe (half-open).
func (b *enrichmentBreaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case breakerOpen:
		if b.now().Sub(b.lastFailedAt) > enrichmentOpenDuration {
			b.state = breakerHalfOpen
			b.probing = true
			slog.Warn("now-playing enrichment breaker half-open (probing catalog recovery)")
			return true
		}
		return false
	case breakerHalfOpen:
		if b.probing {
			return false
		}
		b.probing = true
		return true
	default:
		return true
	}
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
	b.probing = false
}

// recordFailure counts a failed call and trips the breaker open once the
// failure run crosses the threshold, or immediately if a half-open probe fails.
func (b *enrichmentBreaker) recordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.failures++
	b.lastFailedAt = b.now()
	b.probing = false

	if b.state != breakerOpen && (b.state == breakerHalfOpen || b.failures >= enrichmentFailureThreshold) {
		b.state = breakerOpen
		slog.Warn("now-playing enrichment breaker opened (catalog failing)", "failures", b.failures)
	}
}
