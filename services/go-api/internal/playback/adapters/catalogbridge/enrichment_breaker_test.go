package catalogbridge

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

// gaugeRaceAttempts is the repeat bound these interleavings need: the losing
// order is a timing race, so a single attempt proves nothing either way.
const gaugeRaceAttempts = 5000

// breakerGauge is a ports.EnrichmentMetrics double that keeps only the last
// degraded-state edge written to it — all an operator reads off the gauge is
// whichever edge landed last.
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

// failEnoughToTrip records exactly the run of dependency failures that trips a
// closed breaker, and not one more: a further failure after a concurrent
// success cleared the run would re-trip and paper over an out-of-order edge.
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

// awaitDegraded blocks until the breaker has tripped, so the success that
// follows is ordered after the trip and only the gauge edges are left racing.
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

// assertGaugeMatchesState fails unless the last edge the breaker wrote is the
// state it actually ended in. The gauge is read as "is enrichment fast-failing
// right now", so a trailing edge from a lost race is not a blip: nothing
// rewrites it until the next transition.
func assertGaugeMatchesState(t *testing.T, attempt int, gauge *breakerGauge, b *enrichmentBreaker) {
	t.Helper()
	if gauge.readsDegraded() != breakerIsDegraded(b) {
		t.Fatalf("attempt %d: gauge reads degraded=%v while the breaker is degraded=%v",
			attempt, gauge.readsDegraded(), breakerIsDegraded(b))
	}
}

// TestBreaker_GaugeMatchesStateWhenASuccessRacesTheTrippingFailure reproduces
// the reported defect: a lookup that succeeds and one that trips the breaker
// wrote their gauge edges after releasing the state lock, so the healthy edge
// could land last while the breaker stayed open. That latches the gauge to
// healthy for the rest of the outage — an already-open breaker never announces
// itself again — which is the exact window the gauge exists to make visible.
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

// TestBreaker_GaugeMatchesStateWhenARecoverySuccessFollowsTheTrip pins the
// other direction, where the transitions are ordered and only the edges race: a
// lookup still in flight succeeds just after the breaker tripped, so the
// breaker closes, and the trip's degraded edge must not arrive after the
// recovery's healthy one and leave the gauge crying outage over a live catalog.
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
