package leader

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const testKey int64 = 8_246_113_907_441_099

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func awaitLeader(t *testing.T, e *Election, within time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if e.IsLeader() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func TestElection_OnlyOneInstanceWins(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	first := fastElection(pool)
	first.Start(ctx)
	t.Cleanup(func() { first.Shutdown(ctx) })

	if !awaitLeader(t, first, 2*time.Second) {
		t.Fatal("first election never acquired the lock")
	}

	second := fastElection(pool)
	second.Start(ctx)
	t.Cleanup(func() { second.Shutdown(ctx) })

	if awaitLeader(t, second, 500*time.Millisecond) {
		t.Fatal("second election acquired a lock the first already holds")
	}
}

func TestElection_SuccessorTakesOverAfterShutdown(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	outgoing := fastElection(pool)
	outgoing.Start(ctx)
	if !awaitLeader(t, outgoing, 2*time.Second) {
		t.Fatal("outgoing election never acquired the lock")
	}

	incoming := fastElection(pool)
	incoming.Start(ctx)
	t.Cleanup(func() { incoming.Shutdown(ctx) })

	if incoming.IsLeader() {
		t.Fatal("incoming election is leader while the outgoing one still holds the lock")
	}

	outgoing.Shutdown(ctx)

	if !awaitLeader(t, incoming, 3*time.Second) {
		t.Fatal("incoming election never took over after the outgoing one released")
	}
}

func TestElection_AwaitUnblocksOnAcquire(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	e := fastElection(pool)
	e.Start(ctx)
	t.Cleanup(func() { e.Shutdown(ctx) })

	awaitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if !e.Await(awaitCtx) {
		t.Fatal("Await did not unblock after the lock was acquired")
	}
}

func TestElection_AwaitReturnsFalseWhenNeverLeader(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	holder := fastElection(pool)
	holder.Start(ctx)
	t.Cleanup(func() { holder.Shutdown(ctx) })
	if !awaitLeader(t, holder, 2*time.Second) {
		t.Fatal("holder never acquired the lock")
	}

	standby := fastElection(pool)
	standby.Start(ctx)
	t.Cleanup(func() { standby.Shutdown(ctx) })

	awaitCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()

	if standby.Await(awaitCtx) {
		t.Fatal("Await reported leadership the standby never held")
	}
}

func TestElection_ShutdownWithoutStartIsSafe(t *testing.T) {
	e := NewElection(nil, testKey)
	e.Shutdown(context.Background())
}

// errConnector never hands out a connection, standing in for an exhausted pool,
// a rejected password, or a Postgres that is simply down.
type errConnector struct{ err error }

func (c errConnector) Acquire(context.Context) (heldConn, error) { return nil, c.err }

// refusingConn hands back no advisory lock. With lockErr nil the lock is merely
// held by another instance — the benign outcome of every standby's tick; with
// lockErr set the lock query itself failed.
type refusingConn struct {
	lockErr  error
	released bool
}

func (c *refusingConn) TryAdvisoryLock(context.Context, int64) (bool, error) {
	return false, c.lockErr
}

func (c *refusingConn) Ping(context.Context) error                  { return nil }
func (c *refusingConn) AdvisoryUnlock(context.Context, int64) error { return nil }
func (c *refusingConn) Release()                                    { c.released = true }

// captureWarnings redirects the default logger for one test and returns what a
// production operator would see at warn level and above.
func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(restore) })
	return &buf
}

func electionOver(db connector) *Election {
	return &Election{db: db, key: testKey, interval: 20 * time.Millisecond, won: make(chan struct{})}
}

// TestElection_PoolFailureIsLoggedAndCounted is the #1604 regression: a pool
// that cannot hand out a connection left acquire() silent and uncounted, so an
// instance wedged out of the election looked exactly like a healthy standby.
func TestElection_PoolFailureIsLoggedAndCounted(t *testing.T) {
	logged := captureWarnings(t)
	e := electionOver(errConnector{errors.New("pool exhausted")})

	e.acquire(context.Background())

	if !strings.Contains(logged.String(), "leader.acquire_failed") {
		t.Errorf("a failed pool acquire emitted no warning, got: %q", logged.String())
	}
	if !strings.Contains(logged.String(), "pool exhausted") {
		t.Errorf("the acquire warning did not carry the cause, got: %q", logged.String())
	}
	if c := e.Counters(); c.Attempts != 1 || c.Failures != 1 || c.Contended != 0 {
		t.Errorf("counters = %+v, want one attempt counted as a failure, not as contention", c)
	}
}

// TestElection_LockQueryFailureIsLoggedAndCounted covers the second half of the
// same class: the pool served a connection but the advisory-lock query itself
// failed, which is as much a stuck instance as never getting a connection.
func TestElection_LockQueryFailureIsLoggedAndCounted(t *testing.T) {
	logged := captureWarnings(t)
	conn := &refusingConn{lockErr: errors.New("connection reset by peer")}
	e := electionOver(fixedConnector{conn})

	e.acquire(context.Background())

	if !strings.Contains(logged.String(), "leader.acquire_failed") {
		t.Errorf("a failed lock query emitted no warning, got: %q", logged.String())
	}
	if c := e.Counters(); c.Failures != 1 || c.Contended != 0 {
		t.Errorf("counters = %+v, want the failed lock query counted as a failure", c)
	}
	if !conn.released {
		t.Error("the connection was not returned to the pool after the lock query failed")
	}
}

// TestElection_LostRaceIsCountedWithoutWarning is the other arm: losing the lock
// to a live leader is how a standby is supposed to spend every tick, so it must
// stay out of the warning stream that now carries real failures.
func TestElection_LostRaceIsCountedWithoutWarning(t *testing.T) {
	logged := captureWarnings(t)
	conn := &refusingConn{}
	e := electionOver(fixedConnector{conn})

	e.acquire(context.Background())

	if logged.String() != "" {
		t.Errorf("losing the race to another instance warned, got: %q", logged.String())
	}
	if c := e.Counters(); c.Contended != 1 || c.Failures != 0 {
		t.Errorf("counters = %+v, want the lost race counted as contention, not as a failure", c)
	}
	if !conn.released {
		t.Error("the connection was not returned to the pool after losing the race")
	}
}

// TestElection_CountsWinsAndTermEnds proves the flapping signal: an instance
// that wins the lock and then loses its session leaves both halves of the round
// trip visible, which a live-only boolean erases the moment it flips back.
func TestElection_CountsWinsAndTermEnds(t *testing.T) {
	conn := &flakyConn{}
	e := electionOver(fixedConnector{conn})
	settleIntoLeadership(e)

	conn.dead = true
	e.tick(context.Background())

	if c := e.Counters(); c.Wins != 1 || c.TermEnds != 1 {
		t.Errorf("counters = %+v, want one win followed by one term end", c)
	}
}

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

// fastElection is a real election with a test-sized interval so the settle
// window (a few intervals) elapses in milliseconds.
func fastElection(pool *pgxpool.Pool) *Election {
	e := NewElection(pool, testKey)
	e.interval = 20 * time.Millisecond
	return e
}

// flakyConn is a held connection whose session can be killed mid-term.
type flakyConn struct{ dead bool }

func (c *flakyConn) TryAdvisoryLock(context.Context, int64) (bool, error) { return true, nil }

func (c *flakyConn) Ping(context.Context) error {
	if c.dead {
		return errors.New("session terminated")
	}
	return nil
}

func (c *flakyConn) AdvisoryUnlock(context.Context, int64) error { return nil }
func (c *flakyConn) Release()                                    {}

type fixedConnector struct{ conn heldConn }

func (f fixedConnector) Acquire(context.Context) (heldConn, error) { return f.conn, nil }

const (
	// predecessorDetectionIntervals is the worst case, in election intervals,
	// for a previous leader to notice its DB session died: one tick to reach its
	// next verify, one for that verify's bounded Ping to fail.
	predecessorDetectionIntervals = 2

	// settledIntervals is how long these tests wait for a won lock to settle.
	// It is a literal multiple of the election interval on purpose: a wait
	// derived from settleWindow() collapses to zero along with the constant it
	// is meant to guard, and passes trivially (issue #1603).
	settledIntervals = 3
)

// TestElection_SettleWindowOutlastsPredecessorDetection pins the constant this
// package's fencing rests on: a window no longer than the predecessor's own
// detection window lets both terms be live at once.
func TestElection_SettleWindowOutlastsPredecessorDetection(t *testing.T) {
	e := &Election{key: testKey, interval: 20 * time.Millisecond}

	detection := predecessorDetectionIntervals * e.interval
	if e.settleWindow() <= detection {
		t.Fatalf("settle window = %v, want longer than the predecessor's %v detection window", e.settleWindow(), detection)
	}
}

// settleIntoLeadership drives e from no lock to a settled leadership term.
func settleIntoLeadership(e *Election) {
	e.tick(context.Background())
	time.Sleep(settledIntervals * e.interval)
	e.tick(context.Background())
}

// TestElection_WonLockSettlesBeforeLeadership proves a freshly won lock is not
// leadership yet: a predecessor whose session just died may still be cutting its
// jobs off, so no term (and no LeaderContext) is issued inside the settle window.
func TestElection_WonLockSettlesBeforeLeadership(t *testing.T) {
	e := &Election{db: fixedConnector{&flakyConn{}}, key: testKey, interval: 20 * time.Millisecond, won: make(chan struct{})}

	e.tick(context.Background())
	if !e.holding() {
		t.Fatal("election did not take the free lock")
	}
	if e.IsLeader() {
		t.Fatal("election claimed leadership inside the settle window")
	}
	if _, _, ok := e.LeaderContext(context.Background()); ok {
		t.Fatal("LeaderContext issued inside the settle window")
	}

	e.tick(context.Background())
	if e.IsLeader() {
		t.Fatal("a verify tick inside the settle window granted leadership")
	}

	time.Sleep(settledIntervals * e.interval)
	e.tick(context.Background())
	if !e.IsLeader() {
		t.Fatalf("election never settled into leadership %d intervals after winning the lock", settledIntervals)
	}
}

// TestElection_SessionDeathCancelsInFlightLeaderContext is the #1016 unit
// regression: a job running under LeaderContext must be canceled, with cause
// ErrLeadershipLost, as soon as the election detects its session died, rather
// than running on to completion alongside the next leader.
func TestElection_SessionDeathCancelsInFlightLeaderContext(t *testing.T) {
	fc := &flakyConn{}
	e := &Election{db: fixedConnector{fc}, key: testKey, interval: time.Millisecond, won: make(chan struct{})}
	settleIntoLeadership(e)

	jobCtx, release, ok := e.LeaderContext(context.Background())
	if !ok {
		t.Fatal("settled leader was refused a LeaderContext")
	}
	defer release()

	fc.dead = true
	e.tick(context.Background())

	select {
	case <-jobCtx.Done():
	default:
		t.Fatal("in-flight job context survived the loss of leadership")
	}
	if cause := context.Cause(jobCtx); !errors.Is(cause, ErrLeadershipLost) {
		t.Fatalf("job context cause = %v, want ErrLeadershipLost", cause)
	}
}

// TestElection_ReleasedLeaderContextDoesNotLeak checks that release detaches the
// term watcher without disturbing the term itself.
func TestElection_ReleasedLeaderContextDoesNotLeak(t *testing.T) {
	e := &Election{db: fixedConnector{&flakyConn{}}, key: testKey, interval: time.Millisecond, won: make(chan struct{})}
	settleIntoLeadership(e)

	jobCtx, release, ok := e.LeaderContext(context.Background())
	if !ok {
		t.Fatal("settled leader was refused a LeaderContext")
	}
	release()
	if !errors.Is(context.Cause(jobCtx), context.Canceled) {
		t.Fatalf("released context cause = %v, want context.Canceled", context.Cause(jobCtx))
	}
	if !e.IsLeader() {
		t.Fatal("releasing one job context ended the whole term")
	}
}

// TestElection_SessionKilledMidJob_StaleWriteNotApplied is the #1016 end-to-end
// regression against a real Postgres: instance A's lock-holding session is
// terminated while a job is in flight; Postgres frees the advisory lock at once
// and instance B wins it. By the time B is leader, A's job context must already
// be canceled, so the stale job's write is refused and only B's run lands.
func TestElection_SessionKilledMidJob_StaleWriteNotApplied(t *testing.T) {
	admin := testPool(t)
	ctx := context.Background()
	mustExec(t, admin, "create table if not exists leader_fencing_1016 (writer text not null)")
	mustExec(t, admin, "truncate leader_fencing_1016")
	t.Cleanup(func() { _, _ = admin.Exec(ctx, "drop table if exists leader_fencing_1016") })

	// A and B share the production interval ratio, just scaled down; A has just
	// verified when the kill lands, so it will not notice for ~one interval.
	a := fastElection(testPool(t))
	a.interval = 100 * time.Millisecond
	a.Start(ctx)
	t.Cleanup(func() { a.Shutdown(ctx) })
	if !awaitLeader(t, a, 2*time.Second) {
		t.Fatal("instance A never became leader")
	}
	staleCtx, releaseStale, ok := a.LeaderContext(ctx)
	if !ok {
		t.Fatal("leader A was refused a LeaderContext")
	}
	defer releaseStale()

	// A's job is mid-flight (it has not written yet) when its session dies.
	terminateLockHolder(t, admin)

	b := fastElection(testPool(t))
	b.interval = 100 * time.Millisecond
	b.Start(ctx)
	t.Cleanup(func() { b.Shutdown(ctx) })
	if !awaitLeader(t, b, 3*time.Second) {
		t.Fatal("instance B never took over after A's session died")
	}
	if staleCtx.Err() == nil {
		t.Fatal("B became leader while A's in-flight job context was still live")
	}

	freshCtx, releaseFresh, ok := b.LeaderContext(ctx)
	if !ok {
		t.Fatal("leader B was refused a LeaderContext")
	}
	defer releaseFresh()
	if _, err := admin.Exec(freshCtx, "insert into leader_fencing_1016 values ('B')"); err != nil {
		t.Fatalf("new leader's write failed: %v", err)
	}

	// A's job now reaches its write, still under its own (stale) job context.
	if _, err := admin.Exec(staleCtx, "insert into leader_fencing_1016 values ('A')"); err == nil {
		t.Fatal("stale leader's write was accepted after the new leader took over")
	}

	var writers []string
	rows, err := admin.Query(ctx, "select writer from leader_fencing_1016 order by writer")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var w string
		if err := rows.Scan(&w); err != nil {
			t.Fatal(err)
		}
		writers = append(writers, w)
	}
	if len(writers) != 1 || writers[0] != "B" {
		t.Fatalf("applied writes = %v, want only the new leader's [B]", writers)
	}
}

func mustExec(t *testing.T, pool *pgxpool.Pool, sql string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

// terminateLockHolder kills the backend session holding testKey's advisory lock,
// modelling a partition/OOM that ends the leader's DB session involuntarily.
func terminateLockHolder(t *testing.T, admin *pgxpool.Pool) {
	t.Helper()
	var killed bool
	err := admin.QueryRow(context.Background(), `
		select coalesce(bool_or(pg_terminate_backend(pid)), false) from pg_locks
		where locktype = 'advisory' and granted and objsubid = 1
		  and ((classid::bigint << 32) | objid::bigint) = $1`, testKey).Scan(&killed)
	if err != nil || !killed {
		t.Fatalf("could not terminate the lock-holding session (killed=%v): %v", killed, err)
	}
	// Termination is asynchronous; wait until Postgres has actually freed the
	// lock so a successor can win it immediately.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var held bool
		err := admin.QueryRow(context.Background(), `
			select exists(select 1 from pg_locks
			where locktype = 'advisory' and granted and objsubid = 1
			  and ((classid::bigint << 32) | objid::bigint) = $1)`, testKey).Scan(&held)
		if err == nil && !held {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("advisory lock still held after terminating its session")
}
