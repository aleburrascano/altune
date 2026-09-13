package leader

import (
	"context"
	"testing"
	"time"
)

// fakeConn stands in for a pooled connection so the timing behaviour of the
// election can be driven without a live Postgres. Its methods observe the
// context they are handed, mirroring how pgx fails fast on an expired one.
type fakeConn struct {
	pingBlocks      bool
	unlockAttempted bool
	unlockErr       error
	unlocked        bool
	released        bool
}

func (c *fakeConn) TryAdvisoryLock(context.Context, int64) (bool, error) { return true, nil }

func (c *fakeConn) Ping(ctx context.Context) error {
	if !c.pingBlocks {
		return nil
	}
	<-ctx.Done()
	return ctx.Err()
}

func (c *fakeConn) AdvisoryUnlock(ctx context.Context, _ int64) error {
	c.unlockAttempted = true
	if err := ctx.Err(); err != nil {
		c.unlockErr = err
		return err
	}
	c.unlocked = true
	return nil
}

func (c *fakeConn) Release() { c.released = true }

func leaderHolding(conn heldConn) *Election {
	return &Election{
		key:      testKey,
		interval: 100 * time.Millisecond,
		won:      make(chan struct{}),
		conn:     conn,
	}
}

// TestElection_ShutdownReleasesLockWithFreshDeadline reproduces gap #2: when the
// wait phase drains the per-component budget, Shutdown must still release the
// advisory lock on a fresh deadline rather than reusing the expired context.
func TestElection_ShutdownReleasesLockWithFreshDeadline(t *testing.T) {
	fc := &fakeConn{}
	e := leaderHolding(fc)

	// Emulate app.go handing Shutdown a budget already exhausted mid-wait.
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	e.Shutdown(expired)

	if !fc.unlockAttempted {
		t.Fatal("shutdown never attempted to release the advisory lock")
	}
	if fc.unlockErr != nil {
		t.Fatalf("unlock ran on an already-expired context: %v", fc.unlockErr)
	}
	if !fc.unlocked {
		t.Fatal("advisory lock not released before the connection returned to the pool")
	}
	if !fc.released {
		t.Fatal("connection was not returned to the pool")
	}
}

// TestElection_VerifyDoesNotFreezeOnStuckPing reproduces gap #1: a wedged Ping
// under the loop's long-lived context froze the single-goroutine loop forever.
// A per-operation deadline must abandon the stuck call and yield leadership.
func TestElection_VerifyDoesNotFreezeOnStuckPing(t *testing.T) {
	fc := &fakeConn{pingBlocks: true}
	e := leaderHolding(fc)

	done := make(chan struct{})
	// context.Background() stands in for the loop's open-ended lifetime context.
	go func() { defer close(done); e.verify(context.Background()) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("verify never returned: a stuck Ping froze the single-goroutine loop")
	}

	if e.IsLeader() {
		t.Fatal("verify kept leadership despite an unresponsive connection")
	}
	if !fc.released {
		t.Fatal("the wedged connection was not released after leadership was lost")
	}
}
