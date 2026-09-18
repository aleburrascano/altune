package leader

import (
	"context"
	"strings"
	"sync"
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

// blockingPingConn keeps its Ping in flight until the test lets it finish, and
// records any pool-facing call that lands while it is. That makes a connection
// handed back mid-Ping an assertion rather than a race the runtime may or may
// not happen to report.
type blockingPingConn struct {
	pinging chan struct{}
	unblock chan struct{}
	once    sync.Once

	mu             sync.Mutex
	midPing        bool
	touchedMidPing bool
	unlocked       bool
	released       bool
}

func newBlockingPingConn() *blockingPingConn {
	return &blockingPingConn{pinging: make(chan struct{}), unblock: make(chan struct{})}
}

func (c *blockingPingConn) TryAdvisoryLock(context.Context, int64) (bool, error) { return true, nil }

func (c *blockingPingConn) Ping(context.Context) error {
	c.setMidPing(true)
	defer c.setMidPing(false)
	c.once.Do(func() { close(c.pinging) })
	<-c.unblock
	return nil
}

func (c *blockingPingConn) AdvisoryUnlock(context.Context, int64) error {
	c.touch(&c.unlocked)
	return nil
}

func (c *blockingPingConn) Release() { c.touch(&c.released) }

func (c *blockingPingConn) setMidPing(inFlight bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.midPing = inFlight
}

// touch records a pool-facing call, flagging it when a Ping still owns the
// connection.
func (c *blockingPingConn) touch(call *bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	*call = true
	c.touchedMidPing = c.touchedMidPing || c.midPing
}

func (c *blockingPingConn) usedWhilePinging() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.touchedMidPing
}

// releasedLock reports the pair that completes a clean shutdown: the advisory
// lock unlocked and the connection handed back to the pool.
func (c *blockingPingConn) releasedLock() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.unlocked && c.released
}

// TestElection_ShutdownReleasesOnlyAfterInFlightVerifyFinishes is the #1605
// regression: Shutdown released the held connection the moment
// runloop.Background's wait hit the caller's budget, so a verify still inside
// Ping had its connection unlocked and returned to the pgx pool underneath it.
// Production is exactly this shape — a 5s leader-election budget against a Ping
// bounded only by the 10s election interval.
func TestElection_ShutdownReleasesOnlyAfterInFlightVerifyFinishes(t *testing.T) {
	conn := newBlockingPingConn()
	e := &Election{db: fixedConnector{conn}, key: testKey, interval: 20 * time.Millisecond, won: make(chan struct{})}
	e.Start(context.Background())
	<-conn.pinging

	budget, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	returned := make(chan struct{})
	go func() { defer close(returned); e.Shutdown(budget) }()

	select {
	case <-returned:
		t.Fatal("Shutdown returned on a budget the in-flight verify outlasted")
	case <-time.After(300 * time.Millisecond):
	}
	if conn.usedWhilePinging() {
		t.Fatal("the held connection went back to the pool while verify's Ping still owned it")
	}

	close(conn.unblock)

	select {
	case <-returned:
	case <-time.After(shutdownLoopBudget):
		t.Fatal("Shutdown never returned after the in-flight verify finished")
	}
	if conn.usedWhilePinging() {
		t.Fatal("the held connection went back to the pool while verify's Ping still owned it")
	}
	if !conn.releasedLock() {
		t.Fatal("shutdown left the advisory lock held although the loop had finished")
	}
}

// lateConnector wins the lock only once the test lets it, standing in for a
// TryAdvisoryLock still in flight when shutdown lands.
type lateConnector struct {
	conn      heldConn
	acquiring chan struct{}
	unblock   chan struct{}
	once      sync.Once
}

func (c *lateConnector) Acquire(context.Context) (heldConn, error) {
	c.once.Do(func() { close(c.acquiring) })
	<-c.unblock
	return c.conn, nil
}

// TestElection_ShutdownReleasesALockWonWhileItWasRunning widens #1605 past the
// verify half: the loop can equally be inside acquire when shutdown lands, and
// a lock won a moment later then had no loop left to release it — the
// connection stayed checked out and leadership could not move to another
// instance until this process died.
func TestElection_ShutdownReleasesALockWonWhileItWasRunning(t *testing.T) {
	conn := &fakeConn{}
	db := &lateConnector{conn: conn, acquiring: make(chan struct{}), unblock: make(chan struct{})}
	e := &Election{db: db, key: testKey, interval: 20 * time.Millisecond, won: make(chan struct{})}
	e.Start(context.Background())
	<-db.acquiring

	budget, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	go func() { time.Sleep(100 * time.Millisecond); close(db.unblock) }() // the lock lands mid-shutdown
	e.Shutdown(budget)

	if !conn.unlocked || !conn.released {
		t.Fatal("a lock won while shutdown was under way was left held, with no loop left to release it")
	}
}

// TestElection_ShutdownKeepsTheLockWhenTheLoopNeverStops is the other arm: a
// tick that never returns (a driver deaf to its canceled context) must not
// extend Shutdown forever, and must cost the lock rather than the connection.
// The lock clears when this instance's DB session does; a connection released
// under a live caller corrupts the pool.
func TestElection_ShutdownKeepsTheLockWhenTheLoopNeverStops(t *testing.T) {
	logged := captureWarnings(t)
	conn := &fakeConn{}
	e := leaderHolding(conn)
	e.loopDone = make(chan struct{}) // a loop goroutine that never returns

	start := time.Now()
	e.Shutdown(context.Background())

	if waited := time.Since(start); waited < shutdownLoopBudget {
		t.Fatalf("Shutdown gave up after %v, before the loop had its %v", waited, shutdownLoopBudget)
	}
	if conn.unlockAttempted || conn.released {
		t.Fatal("shutdown released a connection the loop goroutine may still be using")
	}
	if !strings.Contains(logged.String(), "leader.release_skipped") {
		t.Errorf("a retained lock was not reported to operators, got: %q", logged.String())
	}
}
