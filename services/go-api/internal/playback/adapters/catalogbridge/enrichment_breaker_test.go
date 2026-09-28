package catalogbridge

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

const gaugeRaceAttempts = 5000

type breakerGauge struct {
	mu       sync.Mutex
	degraded bool
}

func (g *breakerGauge) EnrichmentFailed()          {}
func (g *breakerGauge) NowPlayingLookupTimedOut()  {}
func (g *breakerGauge) EnrichmentBreakerRejected() {}
func (g *breakerGauge) EnrichmentBreakerOpened()   { g.write(true) }
func (g *breakerGauge) EnrichmentBreakerClosed()   { g.write(false) }

func (g *breakerGauge) write(degraded bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.degraded = degraded
}

func (g *breakerGauge) readsDegraded() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.degraded
}

func failEnoughToTrip(b *enrichmentBreaker) {
	for i := 0; i < enrichmentFailureThreshold; i++ {
		b.recordFailure()
	}
}

func breakerIsDegraded(b *enrichmentBreaker) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state != breakerClosed
}

func awaitDegraded(t *testing.T, b *enrichmentBreaker) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !breakerIsDegraded(b) {
		if time.Now().After(deadline) {
			t.Error("breaker never tripped on a full run of dependency failures")
			return
		}
		runtime.Gosched()
	}
}

func assertGaugeMatchesState(t *testing.T, attempt int, gauge *breakerGauge, b *enrichmentBreaker) {
	t.Helper()
	if gauge.readsDegraded() != breakerIsDegraded(b) {
		t.Fatalf("attempt %d: gauge reads degraded=%v while the breaker is degraded=%v",
			attempt, gauge.readsDegraded(), breakerIsDegraded(b))
	}
}

func TestBreaker_GaugeMatchesStateWhenASuccessRacesTheTrippingFailure(t *testing.T) {
	captureLogs(t)

	for attempt := 0; attempt < gaugeRaceAttempts; attempt++ {
		gauge := &breakerGauge{}
		breaker := newEnrichmentBreaker(gauge)
		failEnoughToTrip(breaker)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			breaker.recordSuccess()
		}()
		go func() {
			defer wg.Done()
			failEnoughToTrip(breaker)
		}()
		wg.Wait()

		assertGaugeMatchesState(t, attempt, gauge, breaker)
	}
}

func TestBreaker_GaugeMatchesStateWhenARecoverySuccessFollowsTheTrip(t *testing.T) {
	captureLogs(t)

	for attempt := 0; attempt < gaugeRaceAttempts; attempt++ {
		gauge := &breakerGauge{}
		breaker := newEnrichmentBreaker(gauge)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			failEnoughToTrip(breaker)
		}()
		go func() {
			defer wg.Done()
			awaitDegraded(t, breaker)
			breaker.recordSuccess()
		}()
		wg.Wait()

		assertGaugeMatchesState(t, attempt, gauge, breaker)
	}
}
