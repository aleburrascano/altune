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
		if e.isLeader() {
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

	if incoming.isLeader() {
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

type errConnector struct{ err error }

func (c errConnector) Acquire(context.Context) (heldConn, error) { return nil, c.err }

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

func TestElection_ShutdownReleasesLockWithFreshDeadline(t *testing.T) {
	fc := &fakeConn{}
	e := leaderHolding(fc)

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

func TestElection_VerifyDoesNotFreezeOnStuckPing(t *testing.T) {
	fc := &fakeConn{pingBlocks: true}
	e := leaderHolding(fc)

	done := make(chan struct{})
	go func() { defer close(done); e.verify(context.Background()) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("verify never returned: a stuck Ping froze the single-goroutine loop")
	}

	if e.isLeader() {
		t.Fatal("verify kept leadership despite an unresponsive connection")
	}
	if !fc.released {
		t.Fatal("the wedged connection was not released after leadership was lost")
	}
}

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

func (c *blockingPingConn) releasedLock() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.unlocked && c.released
}

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

func TestElection_ShutdownReleasesALockWonWhileItWasRunning(t *testing.T) {
	conn := &fakeConn{}
	db := &lateConnector{conn: conn, acquiring: make(chan struct{}), unblock: make(chan struct{})}
	e := &Election{db: db, key: testKey, interval: 20 * time.Millisecond, won: make(chan struct{})}
	e.Start(context.Background())
	<-db.acquiring

	budget, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	go func() { time.Sleep(100 * time.Millisecond); close(db.unblock) }()
	e.Shutdown(budget)

	if !conn.unlocked || !conn.released {
		t.Fatal("a lock won while shutdown was under way was left held, with no loop left to release it")
	}
}

func TestElection_ShutdownKeepsTheLockWhenTheLoopNeverStops(t *testing.T) {
	logged := captureWarnings(t)
	conn := &fakeConn{}
	e := leaderHolding(conn)
	e.loopDone = make(chan struct{})

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

func fastElection(pool *pgxpool.Pool) *Election {
	e := NewElection(pool, testKey)
	e.interval = 20 * time.Millisecond
	return e
}

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
	predecessorDetectionIntervals = 2

	settledIntervals = 3
)

func TestElection_SettleWindowOutlastsPredecessorDetection(t *testing.T) {
	e := &Election{key: testKey, interval: 20 * time.Millisecond}

	detection := predecessorDetectionIntervals * e.interval
	if e.settleWindow() <= detection {
		t.Fatalf("settle window = %v, want longer than the predecessor's %v detection window", e.settleWindow(), detection)
	}
}

func settleIntoLeadership(e *Election) {
	e.tick(context.Background())
	time.Sleep(settledIntervals * e.interval)
	e.tick(context.Background())
}

func TestElection_WonLockSettlesBeforeLeadership(t *testing.T) {
	e := &Election{db: fixedConnector{&flakyConn{}}, key: testKey, interval: 20 * time.Millisecond, won: make(chan struct{})}

	e.tick(context.Background())
	if !e.holding() {
		t.Fatal("election did not take the free lock")
	}
	if e.isLeader() {
		t.Fatal("election claimed leadership inside the settle window")
	}
	if _, _, ok := e.LeaderContext(context.Background()); ok {
		t.Fatal("LeaderContext issued inside the settle window")
	}

	e.tick(context.Background())
	if e.isLeader() {
		t.Fatal("a verify tick inside the settle window granted leadership")
	}

	time.Sleep(settledIntervals * e.interval)
	e.tick(context.Background())
	if !e.isLeader() {
		t.Fatalf("election never settled into leadership %d intervals after winning the lock", settledIntervals)
	}
}

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
	if !e.isLeader() {
		t.Fatal("releasing one job context ended the whole term")
	}
}

func TestElection_SessionKilledMidJob_StaleWriteNotApplied(t *testing.T) {
	admin := testPool(t)
	ctx := context.Background()
	mustExec(t, admin, "create table if not exists leader_fencing_1016 (writer text not null)")
	mustExec(t, admin, "truncate leader_fencing_1016")
	t.Cleanup(func() { _, _ = admin.Exec(ctx, "drop table if exists leader_fencing_1016") })

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

type switchableConn struct {
	mu      sync.Mutex
	pingErr error
}

func (c *switchableConn) failPings(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pingErr = err
}

func (c *switchableConn) TryAdvisoryLock(context.Context, int64) (bool, error) { return true, nil }

func (c *switchableConn) Ping(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pingErr
}

func (c *switchableConn) AdvisoryUnlock(context.Context, int64) error { return nil }
func (c *switchableConn) Release()                                    {}

func TestElection_FailedPingIsLoggedWithCauseAndCounted(t *testing.T) {
	cases := []struct {
		name    string
		settled bool
	}{
		{"after settle", true},
		{"inside settle window", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logged := captureWarnings(t)
			conn := &switchableConn{}
			e := electionOver(fixedConnector{conn})
			e.Start(context.Background())
			t.Cleanup(func() { e.Shutdown(context.Background()) })
			if tc.settled && !awaitLeader(t, e, 5*time.Second) {
				t.Fatal("election never settled into leadership")
			}

			conn.failPings(errors.New("connection reset by peer"))

			deadline := time.Now().Add(5 * time.Second)
			for e.Counters().HoldsLost == 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if got := e.Counters().HoldsLost; got != 1 {
				t.Errorf("HoldsLost = %d, want 1", got)
			}
			if !strings.Contains(logged.String(), "connection reset by peer") {
				t.Errorf("leader.lost warning did not carry the ping error, got: %q", logged.String())
			}
		})
	}
}

type pingSignalConn struct {
	fakeConn
	pinging chan struct{}
}

func (c *pingSignalConn) Ping(ctx context.Context) error {
	close(c.pinging)
	return c.fakeConn.Ping(ctx)
}

func TestElection_ShutdownDuringVerifyReleasesLockWithoutLostWarning(t *testing.T) {
	warnings := captureWarnings(t)
	conn := &pingSignalConn{fakeConn: fakeConn{pingBlocks: true}, pinging: make(chan struct{})}
	e := leaderHolding(conn)
	e.Start(context.Background())
	<-conn.pinging

	e.Shutdown(context.Background())

	if !conn.unlocked {
		t.Fatalf("advisory lock not unlocked on a live context (unlock error: %v)", conn.unlockErr)
	}
	if strings.Contains(warnings.String(), "leader.lost") {
		t.Fatalf("shutdown was logged as a lost lease: %s", warnings.String())
	}
}
