package breaker_test

import (
	"altune/go-api/internal/shared/breaker"
	"altune/go-api/internal/shared/clock/clocktest"
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	threshold = 5
	openFor   = 30 * time.Second
	lease     = 20 * time.Second
)

type recorder struct {
	mu      sync.Mutex
	changes []breaker.Change
}

func (r *recorder) record(_ context.Context, c breaker.Change) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.changes = append(r.changes, c)
}

func (r *recorder) kinds() []breaker.Kind {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []breaker.Kind
	for _, c := range r.changes {
		out = append(out, c.Kind)
	}
	return out
}

func newBreaker() (*breaker.Breaker, *clocktest.Manual, *recorder) {
	clk := clocktest.NewManual(time.Unix(1_700_000_000, 0))
	rec := &recorder{}
	b := breaker.New(breaker.Config{Threshold: threshold, OpenFor: openFor, ProbeLease: lease, Now: clk.Now, OnChange: rec.record})
	return b, clk, rec
}

func trip(b *breaker.Breaker) {
	for range threshold {
		b.Failure(context.Background(), breaker.Admission{})
	}
}

func probe(t *testing.T, b *breaker.Breaker, clk *clocktest.Manual) breaker.Admission {
	t.Helper()
	trip(b)
	clk.Advance(openFor + time.Nanosecond)
	adm, ok := b.Allow(context.Background())
	if !ok {
		t.Fatal("the probe was refused after the open window")
	}
	return adm
}

func TestBreaker_AClosedCircuitAdmitsEveryCaller(t *testing.T) {
	b, _, _ := newBreaker()
	for range 10 {
		if _, ok := b.Allow(context.Background()); !ok {
			t.Fatal("a closed circuit refused a caller")
		}
	}
	if b.State() != breaker.Closed {
		t.Errorf("state = %v, want closed", b.State())
	}
}

func TestBreaker_OpensOnTheFailureThatReachesTheThreshold(t *testing.T) {
	b, _, rec := newBreaker()
	for range threshold - 1 {
		b.Failure(context.Background(), breaker.Admission{})
	}
	if _, ok := b.Allow(context.Background()); !ok {
		t.Fatal("the circuit opened one failure early")
	}

	b.Failure(context.Background(), breaker.Admission{})

	if _, ok := b.Allow(context.Background()); ok {
		t.Error("the circuit stayed closed at the threshold")
	}
	if b.State() != breaker.Open {
		t.Errorf("state = %v, want open", b.State())
	}
	if got := rec.changes; len(got) != 1 || got[0] != (breaker.Change{Kind: breaker.Tripped, Failures: threshold}) {
		t.Errorf("changes = %v, want one trip at %d failures", got, threshold)
	}
}

func TestBreaker_ASuccessWhileClosedResetsTheFailureCount(t *testing.T) {
	b, _, _ := newBreaker()
	adm, _ := b.Allow(context.Background())
	for range threshold - 1 {
		b.Failure(context.Background(), breaker.Admission{})
	}

	b.Success(context.Background(), adm)
	for range threshold - 1 {
		b.Failure(context.Background(), breaker.Admission{})
	}

	if b.State() != breaker.Closed {
		t.Errorf("state = %v, want the count to have restarted so the circuit stays closed", b.State())
	}
}

func TestBreaker_AnOpenCircuitRefusesUntilTheWindowHasPassed(t *testing.T) {
	b, clk, _ := newBreaker()
	trip(b)

	clk.Advance(openFor)
	if _, ok := b.Allow(context.Background()); ok {
		t.Fatal("the circuit admitted a caller at exactly the open window")
	}

	clk.Advance(time.Nanosecond)
	if _, ok := b.Allow(context.Background()); !ok {
		t.Error("the circuit refused the probe once the window had passed")
	}
	if b.State() != breaker.HalfOpen {
		t.Errorf("state = %v, want half-open", b.State())
	}
}

func TestBreaker_AFailureWhileOpenExtendsTheWindow(t *testing.T) {
	b, clk, _ := newBreaker()
	trip(b)
	clk.Advance(openFor - time.Second)

	b.Failure(context.Background(), breaker.Admission{})
	clk.Advance(2 * time.Second)

	if _, ok := b.Allow(context.Background()); ok {
		t.Error("a failure recorded while open did not restart the window")
	}
}

func TestBreaker_HalfOpenAdmitsExactlyOneConcurrentProbe(t *testing.T) {
	b, clk, _ := newBreaker()
	trip(b)
	clk.Advance(openFor + time.Second)

	var admitted atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := b.Allow(context.Background()); ok {
				admitted.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := admitted.Load(); got != 1 {
		t.Errorf("half-open admitted %d probes, want exactly 1", got)
	}
}

func TestBreaker_AProbeSuccessClosesTheCircuitAndResetsTheCount(t *testing.T) {
	b, clk, rec := newBreaker()
	adm := probe(t, b, clk)

	b.Success(context.Background(), adm)

	if b.State() != breaker.Closed {
		t.Fatalf("state = %v, want closed", b.State())
	}
	if got := b.Snapshot(); got.Failures != 0 || got.Probing {
		t.Errorf("snapshot = %+v, want no failures and no probe in flight", got)
	}
	want := []breaker.Kind{breaker.Tripped, breaker.Probing, breaker.Recovered}
	if got := rec.kinds(); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("changes = %v, want %v", got, want)
	}
}

func TestBreaker_AProbeFailureReopensAtOnce(t *testing.T) {
	b, clk, _ := newBreaker()
	adm := probe(t, b, clk)

	b.Failure(context.Background(), adm)

	if b.State() != breaker.Open {
		t.Fatalf("state = %v, want open", b.State())
	}
	if _, ok := b.Allow(context.Background()); ok {
		t.Error("the circuit admitted a caller right after a failed probe")
	}
	if b.Snapshot().Probing {
		t.Error("a failed probe was left marked in flight")
	}
}

func TestBreaker_ASuccessAdmittedBeforeTheTripCannotCloseAnOpenCircuit(t *testing.T) {
	b, _, _ := newBreaker()
	stale, _ := b.Allow(context.Background())
	trip(b)

	b.Success(context.Background(), stale)

	if b.State() != breaker.Open {
		t.Errorf("state = %v, want a stale success to leave the circuit open", b.State())
	}
}

func TestBreaker_AStaleSuccessDoesNotResetTheCountOfALaterRun(t *testing.T) {
	b, clk, _ := newBreaker()
	stale, _ := b.Allow(context.Background())
	adm := probe(t, b, clk)
	b.Success(context.Background(), adm)
	for range threshold - 1 {
		b.Failure(context.Background(), breaker.Admission{})
	}

	b.Success(context.Background(), stale)
	b.Failure(context.Background(), breaker.Admission{})

	if b.State() != breaker.Open {
		t.Errorf("state = %v, want the stale success to have left the failure count alone", b.State())
	}
}

func TestBreaker_AProbeSuccessAfterTheCircuitReopenedIsIgnored(t *testing.T) {
	b, clk, _ := newBreaker()
	first := probe(t, b, clk)
	b.Failure(context.Background(), breaker.Admission{})
	clk.Advance(openFor + time.Second)
	if _, ok := b.Allow(context.Background()); !ok {
		t.Fatal("the second probe was refused")
	}

	b.Success(context.Background(), first)

	if b.State() != breaker.HalfOpen {
		t.Errorf("state = %v, want the first probe's late success to leave the second probe in charge", b.State())
	}
}

func TestBreaker_ReleasingAProbeAdmitsTheNextOne(t *testing.T) {
	b, clk, _ := newBreaker()
	adm := probe(t, b, clk)
	if _, ok := b.Allow(context.Background()); ok {
		t.Fatal("a second probe was admitted while the first was in flight")
	}

	b.Release(adm)

	if _, ok := b.Allow(context.Background()); !ok {
		t.Error("a released probe slot did not admit the next probe")
	}
	if _, ok := b.Allow(context.Background()); ok {
		t.Error("the re-admitted probe was not exclusive")
	}
}

func TestBreaker_ReleasingACallThatWasNotAProbeChangesNothing(t *testing.T) {
	b, clk, _ := newBreaker()
	closed, _ := b.Allow(context.Background())
	probe(t, b, clk)

	b.Release(closed)

	if _, ok := b.Allow(context.Background()); ok {
		t.Error("releasing an ordinary admission freed the probe slot")
	}
}

func TestBreaker_ReleasingAnOpenCircuitDoesNotLetCallersThrough(t *testing.T) {
	b, _, _ := newBreaker()
	trip(b)

	b.Release(breaker.Admission{})

	if _, ok := b.Allow(context.Background()); ok {
		t.Error("a release let a caller through an open circuit")
	}
}

func TestBreaker_AnAbandonedProbeLeaseExpiresAfterTheLease(t *testing.T) {
	b, clk, rec := newBreaker()
	probe(t, b, clk)

	clk.Advance(lease)
	if _, ok := b.Allow(context.Background()); ok {
		t.Fatal("a probe was re-admitted at exactly the lease")
	}

	clk.Advance(time.Nanosecond)
	if _, ok := b.Allow(context.Background()); !ok {
		t.Fatal("an expired lease did not admit a new probe")
	}
	if _, ok := b.Allow(context.Background()); ok {
		t.Error("the new probe did not hold a fresh lease")
	}
	kinds := rec.kinds()
	if len(kinds) == 0 || kinds[len(kinds)-1] != breaker.LeaseExpired {
		t.Errorf("changes = %v, want the lease expiry reported", kinds)
	}
}

func TestBreaker_AProbeThatLostItsLeaseCannotFreeTheNewProbesSlot(t *testing.T) {
	b, clk, _ := newBreaker()
	lost := probe(t, b, clk)
	clk.Advance(lease + time.Second)
	if _, ok := b.Allow(context.Background()); !ok {
		t.Fatal("the second probe was refused")
	}

	b.Release(lost)

	if _, ok := b.Allow(context.Background()); ok {
		t.Error("the expired probe's release freed the live probe's slot")
	}
}

func TestBreaker_AFailureFromAProbeThatLostItsLeaseStillReopens(t *testing.T) {
	b, clk, _ := newBreaker()
	lost := probe(t, b, clk)
	clk.Advance(lease + time.Second)
	live, _ := b.Allow(context.Background())

	b.Failure(context.Background(), lost)
	b.Success(context.Background(), live)

	if b.State() != breaker.Open {
		t.Errorf("state = %v, want the late failure to have reopened the circuit and the stale probe's success to be ignored", b.State())
	}
}

func TestBreaker_AThresholdBelowOneCountsAsOne(t *testing.T) {
	b := breaker.New(breaker.Config{Threshold: 0, OpenFor: openFor, ProbeLease: lease})

	b.Failure(context.Background(), breaker.Admission{})

	if b.State() != breaker.Open {
		t.Errorf("state = %v, want one failure to open a breaker configured with no threshold", b.State())
	}
}

func TestBreaker_TheZeroClockIsTheWallClock(t *testing.T) {
	b := breaker.New(breaker.Config{Threshold: 1, OpenFor: time.Hour, ProbeLease: time.Hour})
	b.Failure(context.Background(), breaker.Admission{})

	if _, ok := b.Allow(context.Background()); ok {
		t.Error("a breaker without a clock admitted a caller inside its open window")
	}
}

func TestBreaker_AWalkThroughEveryStateUnderConcurrency(t *testing.T) {
	b, clk, _ := newBreaker()
	hammer := func(n int, f func()) {
		var wg sync.WaitGroup
		for range n {
			wg.Add(1)
			go func() { defer wg.Done(); f() }()
		}
		wg.Wait()
	}

	hammer(16, func() {
		if _, ok := b.Allow(context.Background()); !ok {
			t.Error("a closed circuit refused a caller")
		}
	})
	hammer(threshold, func() { b.Failure(context.Background(), breaker.Admission{}) })
	if _, ok := b.Allow(context.Background()); ok {
		t.Fatal("the circuit stayed closed after the threshold")
	}

	clk.Advance(openFor + time.Second)
	var admitted atomic.Int32
	var live atomic.Value
	hammer(16, func() {
		if adm, ok := b.Allow(context.Background()); ok {
			admitted.Add(1)
			live.Store(adm)
		}
	})
	if admitted.Load() != 1 {
		t.Fatalf("half-open admitted %d probes, want 1", admitted.Load())
	}

	b.Success(context.Background(), live.Load().(breaker.Admission))
	hammer(16, func() {
		if _, ok := b.Allow(context.Background()); !ok {
			t.Error("a recovered circuit refused a caller")
		}
	})
}
