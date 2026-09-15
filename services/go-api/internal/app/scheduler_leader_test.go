package app

import (
	"altune/go-api/internal/shared/leader"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeElection stands in for leader.Election so a handoff can be simulated
// without a live Postgres advisory lock. win/lose flip leadership to model the
// window where one instance loses the lock and another acquires it; like the
// real election, lose cancels every job context issued during the term.
type fakeElection struct {
	mu      sync.Mutex
	leader  bool
	cancels []context.CancelCauseFunc
}

func (f *fakeElection) Start(context.Context) {}

func (f *fakeElection) Await(context.Context) bool { return true }

func (f *fakeElection) LeaderContext(parent context.Context) (context.Context, context.CancelFunc, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.leader {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancelCause(parent)
	f.cancels = append(f.cancels, cancel)
	return ctx, func() { cancel(context.Canceled) }, true
}

func (f *fakeElection) win() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.leader = true
}

func (f *fakeElection) lose() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.leader = false
	for _, cancel := range f.cancels {
		cancel(leader.ErrLeadershipLost)
	}
	f.cancels = nil
}

func (f *fakeElection) Shutdown(context.Context) { f.lose() }

// waitForAtLeast polls an atomic counter until it reaches want or the deadline
// elapses, so the test can prove the job actually ran before the handoff
// instead of racing it.
func waitForAtLeast(t *testing.T, c *atomic.Int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.Load() >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("counter never reached %d (got %d)", want, c.Load())
}

// TestRunTicker_LeaderHandoff_OldLeaderStopsRunning is the regression for #391:
// a leader-gated ticker must re-check leadership on every tick so that, when a
// blue-green deploy hands the lock to a new instance, the old instance stops
// running the job. Before the fix runTicker fired forever once leadership was
// first won, so the old and new leaders ran the same job concurrently.
func TestRunTicker_LeaderHandoff_OldLeaderStopsRunning(t *testing.T) {
	const tick = time.Millisecond

	oldE := &fakeElection{}
	oldE.win()
	newE := &fakeElection{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	oldApp := &App{election: oldE}
	newApp := &App{election: newE}

	var oldRuns, newRuns atomic.Int32
	oldApp.runTicker(ctx, "metrics rollup", tick, func(context.Context) error { oldRuns.Add(1); return nil })

	// The old instance is the sole leader: it must be running the job.
	waitForAtLeast(t, &oldRuns, 3)

	// Blue-green handoff: the old instance loses the lock, the new one wins it
	// and starts its own copy of the same job.
	oldE.lose()
	newE.win()
	newApp.runTicker(ctx, "metrics rollup", tick, func(context.Context) error { newRuns.Add(1); return nil })

	// Let any in-flight tick on the old instance drain.
	time.Sleep(30 * tick)
	baseline := oldRuns.Load()

	// Across many further ticks the old instance must not run the job again.
	time.Sleep(60 * tick)
	extra := oldRuns.Load() - baseline
	if extra > 1 {
		t.Fatalf("old leader kept running the job after handoff: %d extra runs", extra)
	}

	// Sanity: the new leader actually picked the job up.
	waitForAtLeast(t, &newRuns, 3)
}

// TestRunTicker_LeadershipLostMidTick_StaleWriteNotApplied is the regression
// for #1016: leadership used to be re-checked only at the start of a tick, so a
// leader whose DB session died while fn was still running carried on and
// committed its write concurrently with the successor's run of the same job.
// The in-flight run's context must be canceled on the loss, so its write is
// refused and only the new leader's lands.
func TestRunTicker_LeadershipLostMidTick_StaleWriteNotApplied(t *testing.T) {
	oldE := &fakeElection{}
	oldE.win()
	newE := &fakeElection{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		mu      sync.Mutex
		applied []string
	)
	// write models a leader-only job's DB write: like a pgx Exec, it is refused
	// once the context it runs under is done.
	write := func(ctx context.Context, who string) error {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		mu.Lock()
		defer mu.Unlock()
		applied = append(applied, who)
		return nil
	}

	started := make(chan struct{})
	proceed := make(chan struct{})
	staleErr := make(chan error, 1)
	oldApp := &App{election: oldE}
	oldApp.runTicker(ctx, "metrics rollup", time.Hour, func(ctx context.Context) error {
		close(started)
		<-proceed // the job body is still running when the session dies
		err := write(ctx, "old")
		staleErr <- err
		return err
	})
	<-started

	// The old leader's session dies mid-tick; the new leader wins and runs the job.
	oldE.lose()
	newE.win()
	newApp := &App{election: newE}
	newDone := make(chan struct{})
	newApp.runTicker(ctx, "metrics rollup", time.Hour, func(ctx context.Context) error {
		defer close(newDone)
		return write(ctx, "new")
	})
	<-newDone

	close(proceed)
	if err := <-staleErr; !errors.Is(err, leader.ErrLeadershipLost) {
		t.Fatalf("stale leader's write returned %v, want it refused with ErrLeadershipLost", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(applied) != 1 || applied[0] != "new" {
		t.Fatalf("applied writes = %v, want only the new leader's [new]", applied)
	}
}
