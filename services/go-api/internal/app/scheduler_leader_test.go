package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// fakeElection stands in for leader.Election so a handoff can be simulated
// without a live Postgres advisory lock. leadership is flipped atomically to
// model the blue-green window where one instance loses the lock and another
// acquires it.
type fakeElection struct {
	leader atomic.Bool
}

func (f *fakeElection) Start(context.Context) {}

func (f *fakeElection) Await(context.Context) bool { return true }

func (f *fakeElection) IsLeader() bool { return f.leader.Load() }

func (f *fakeElection) Shutdown(context.Context) { f.leader.Store(false) }

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
	oldE.leader.Store(true)
	newE := &fakeElection{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	oldApp := &App{election: oldE}
	newApp := &App{election: newE}

	var oldRuns, newRuns atomic.Int32
	oldApp.runTicker(ctx, "metrics rollup", tick, func() { oldRuns.Add(1) })

	// The old instance is the sole leader: it must be running the job.
	waitForAtLeast(t, &oldRuns, 3)

	// Blue-green handoff: the old instance loses the lock, the new one wins it
	// and starts its own copy of the same job.
	oldE.leader.Store(false)
	newE.leader.Store(true)
	newApp.runTicker(ctx, "metrics rollup", tick, func() { newRuns.Add(1) })

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
