package leader_test

import (
	"altune/go-api/internal/platform/leader"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/dbtest"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/polltest"
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const testKey int64 = 8_246_113_907_441_099

const (
	fastInterval = 20 * time.Millisecond

	databaseInterval = 100 * time.Millisecond

	singleTick = time.Hour

	shutdownLoopBudget = 2 * time.Second

	predecessorDetectionIntervals = 2
)

func awaitLeader(e *leader.Election, within time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), within)
	defer cancel()
	return e.Await(ctx)
}

func isLeader(e *leader.Election) bool {
	_, release, ok := e.LeaderContext(context.Background())
	if ok {
		release()
	}
	return ok
}

func newElection(t *testing.T, locks leader.LockStore, key int64, interval time.Duration) *leader.Election {
	t.Helper()
	e, err := leader.NewElection(locks, key, interval, clock.Now, clock.NewTimer, clock.NewTicker)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestNewElection_RefusesAMissingTicker(t *testing.T) {
	e, err := leader.NewElection(&store{}, testKey, singleTick, clock.Now, clock.NewTimer, nil)

	if e != nil || !errors.Is(err, leader.ErrTickerRequired) || !strings.Contains(err.Error(), "NewElection") {
		t.Fatalf("got (%v, %v), want a nil election and an error naming NewElection wrapping ErrTickerRequired", e, err)
	}
}

func TestNewElection_RefusesAMissingClock(t *testing.T) {
	e, err := leader.NewElection(&store{}, testKey, singleTick, nil, clock.NewTimer, clock.NewTicker)

	if e != nil || !errors.Is(err, leader.ErrClockRequired) || !strings.Contains(err.Error(), "NewElection") {
		t.Fatalf("got (%v, %v), want a nil election and an error naming NewElection wrapping ErrClockRequired", e, err)
	}
}

func TestNewElection_RefusesAMissingTimer(t *testing.T) {
	e, err := leader.NewElection(&store{}, testKey, singleTick, clock.Now, nil, clock.NewTicker)

	if e != nil || !errors.Is(err, leader.ErrTimerRequired) || !strings.Contains(err.Error(), "NewElection") {
		t.Fatalf("got (%v, %v), want a nil election and an error naming NewElection wrapping ErrTimerRequired", e, err)
	}
}

func startElection(t *testing.T, locks leader.LockStore, interval time.Duration) *leader.Election {
	t.Helper()
	e := newElection(t, locks, testKey, interval)
	e.Start(context.Background())
	t.Cleanup(func() { e.Shutdown(context.Background()) })
	return e
}

func fastElection(t *testing.T, pool *pgxpool.Pool, interval time.Duration) *leader.Election {
	t.Helper()
	return startElection(t, leader.AdaptLocks(database.NewAdvisoryLocks(pool).Acquire), interval)
}

type signal struct {
	once sync.Once
	ch   chan struct{}
}

func newSignal() *signal { return &signal{ch: make(chan struct{})} }

func (s *signal) fire() { s.once.Do(func() { close(s.ch) }) }

func (s *signal) wait(t *testing.T, within time.Duration, what string) {
	t.Helper()
	select {
	case <-s.ch:
	case <-time.After(within):
		t.Fatalf("%s did not happen within %v", what, within)
	}
}

type store struct {
	session  leader.LockSession
	err      error
	acquired *signal
}

func storeOf(session leader.LockSession) *store {
	return &store{session: session, acquired: newSignal()}
}

func (s *store) Acquire(context.Context) (leader.LockSession, error) {
	defer s.acquired.fire()
	if s.err != nil {
		return nil, s.err
	}
	return s.session, nil
}

type session struct {
	mu                   sync.Mutex
	lockLost             bool
	lockErr              error
	pingErr              error
	pingWaitsForDeadline bool
	unlockCtxErr         error
	unlockBudget         time.Duration
	unlockValue          any
	unlockAttempted      bool
	unlocked             bool
	released             bool

	locked     *signal
	pinged     *signal
	releasedAt *signal
}

func newSession() *session {
	return &session{locked: newSignal(), pinged: newSignal(), releasedAt: newSignal()}
}

func (s *session) TryAdvisoryLock(context.Context, int64) (bool, error) {
	defer s.locked.fire()
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.lockLost && s.lockErr == nil, s.lockErr
}

func (s *session) Ping(ctx context.Context) error {
	s.pinged.fire()
	s.mu.Lock()
	waits, err := s.pingWaitsForDeadline, s.pingErr
	s.mu.Unlock()
	if waits {
		<-ctx.Done()
		return ctx.Err()
	}
	return err
}

func (s *session) AdvisoryUnlock(ctx context.Context, _ int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unlockAttempted = true
	if deadline, ok := ctx.Deadline(); ok {
		s.unlockBudget = time.Until(deadline)
	}
	s.unlockValue = ctx.Value(budgetKey{})
	if err := ctx.Err(); err != nil {
		s.unlockCtxErr = err
		return err
	}
	s.unlocked = true
	return nil
}

func (s *session) Release() {
	s.mu.Lock()
	s.released = true
	s.mu.Unlock()
	s.releasedAt.fire()
}

func (s *session) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pingErr = err
	s.lockLost = true
}

func (s *session) hang() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pingWaitsForDeadline = true
	s.lockLost = true
}

func (s *session) state() (unlockAttempted, unlocked, released bool, unlockCtxErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unlockAttempted, s.unlocked, s.released, s.unlockCtxErr
}

func captureWarnings(t *testing.T) *logging.Capture {
	t.Helper()
	return captureLogs(t, slog.LevelWarn)
}

func captureLogs(t *testing.T, level slog.Level) *logging.Capture {
	t.Helper()
	return logging.CaptureText(t, level)
}

func runOneTick(t *testing.T, s *store) *leader.Election {
	t.Helper()
	e := newElection(t, s, testKey, singleTick)
	e.Start(context.Background())
	s.acquired.wait(t, 2*time.Second, "the first lock attempt")
	e.Shutdown(context.Background())
	return e
}

func TestElection_OnlyOneInstanceWins(t *testing.T) {
	pool := dbtest.Pool(t)

	first := fastElection(t, pool, databaseInterval)
	if !awaitLeader(first, 5*time.Second) {
		t.Fatal("first election never acquired the lock")
	}

	second := fastElection(t, pool, databaseInterval)
	if awaitLeader(second, time.Second) {
		t.Fatal("second election acquired a lock the first already holds")
	}
}

func TestElection_SuccessorTakesOverAfterShutdown(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()

	outgoing := newElection(t, leader.AdaptLocks(database.NewAdvisoryLocks(pool).Acquire), testKey, databaseInterval)
	outgoing.Start(ctx)
	if !awaitLeader(outgoing, 5*time.Second) {
		t.Fatal("outgoing election never acquired the lock")
	}

	incoming := fastElection(t, pool, databaseInterval)
	if isLeader(incoming) {
		t.Fatal("incoming election is leader while the outgoing one still holds the lock")
	}

	outgoing.Shutdown(ctx)

	if !awaitLeader(incoming, 5*time.Second) {
		t.Fatal("incoming election never took over after the outgoing one released")
	}
}

func TestElection_AwaitUnblocksOnAcquire(t *testing.T) {
	e := fastElection(t, dbtest.Pool(t), databaseInterval)

	if !awaitLeader(e, 5*time.Second) {
		t.Fatal("Await did not unblock after the lock was acquired")
	}
}

func TestElection_AwaitReturnsFalseWhenNeverLeader(t *testing.T) {
	pool := dbtest.Pool(t)

	holder := fastElection(t, pool, databaseInterval)
	if !awaitLeader(holder, 5*time.Second) {
		t.Fatal("holder never acquired the lock")
	}

	standby := fastElection(t, pool, databaseInterval)
	if awaitLeader(standby, time.Second) {
		t.Fatal("Await reported leadership the standby never held")
	}
}

func TestElection_ShutdownWithoutStartIsSafe(t *testing.T) {
	e := newElection(t, &store{}, testKey, singleTick)
	e.Shutdown(context.Background())
}

func TestElection_PoolFailureIsLoggedAndCounted(t *testing.T) {
	logged := captureWarnings(t)

	e := runOneTick(t, &store{err: errors.New("pool exhausted"), acquired: newSignal()})

	if !strings.Contains(logged.String(), "leader.acquire_failed") {
		t.Errorf("a failed pool acquire emitted no warning, got: %q", logged.String())
	}
	if !strings.Contains(logged.String(), "pool exhausted") {
		t.Errorf("the acquire warning did not carry the cause, got: %q", logged.String())
	}
	if c := e.Counters(); c.Attempts != 1 || c.Failures != 1 || c.Contended != 0 || c.Wins != 0 {
		t.Errorf("counters = %+v, want one attempt counted as a failure, not as contention", c)
	}
}

func TestElection_LockQueryFailureIsLoggedAndCounted(t *testing.T) {
	logged := captureWarnings(t)
	conn := newSession()
	conn.lockErr = errors.New("connection reset by peer")

	e := runOneTick(t, storeOf(conn))

	if !strings.Contains(logged.String(), "leader.acquire_failed") {
		t.Errorf("a failed lock query emitted no warning, got: %q", logged.String())
	}
	if c := e.Counters(); c.Attempts != 1 || c.Failures != 1 || c.Contended != 0 || c.Wins != 0 {
		t.Errorf("counters = %+v, want the failed lock query counted as a failure", c)
	}
	if _, _, released, _ := conn.state(); !released {
		t.Error("the connection was not returned to the pool after the lock query failed")
	}
}

func TestElection_LostRaceIsCountedWithoutWarning(t *testing.T) {
	logged := captureWarnings(t)
	conn := newSession()
	conn.lockLost = true

	e := runOneTick(t, storeOf(conn))

	if logged.String() != "" {
		t.Errorf("losing the race to another instance warned, got: %q", logged.String())
	}
	if c := e.Counters(); c.Attempts != 1 || c.Contended != 1 || c.Failures != 0 || c.Wins != 0 {
		t.Errorf("counters = %+v, want the lost race counted as contention, not as a failure", c)
	}
	if _, _, released, _ := conn.state(); !released {
		t.Error("the connection was not returned to the pool after losing the race")
	}
}

func TestElection_WinningTheLockIsCountedAndHeldUntilShutdown(t *testing.T) {
	conn := newSession()

	e := runOneTick(t, storeOf(conn))

	if c := e.Counters(); c.Attempts != 1 || c.Wins != 1 || c.Contended != 0 || c.Failures != 0 {
		t.Errorf("counters = %+v, want one attempt counted as a win", c)
	}
	if _, unlocked, released, _ := conn.state(); !unlocked || !released {
		t.Error("shutdown did not unlock and return the connection it won")
	}
}

func TestElection_CountsWinsAndTermEnds(t *testing.T) {
	conn := newSession()
	e := startElection(t, storeOf(conn), fastInterval)
	if !awaitLeader(e, 5*time.Second) {
		t.Fatal("election never settled into leadership")
	}

	conn.fail(errors.New("session terminated"))
	conn.releasedAt.wait(t, 2*time.Second, "releasing the dead session")
	e.Shutdown(context.Background())

	if c := e.Counters(); c.Wins != 1 || c.TermEnds != 1 || c.HoldsLost != 1 {
		t.Errorf("counters = %+v, want one win followed by one lost hold that ended the term", c)
	}
}

type budgetKey struct{}

func TestElection_ShutdownReleaseKeepsItsFullBudgetAndTheCallersValues(t *testing.T) {
	conn := newSession()
	e := newElection(t, storeOf(conn), testKey, singleTick)
	e.Start(context.Background())
	conn.locked.wait(t, 2*time.Second, "winning the lock")
	parent, cancel := context.WithTimeout(context.WithValue(context.Background(), budgetKey{}, "caller"), time.Nanosecond)
	defer cancel()

	e.Shutdown(parent)

	conn.mu.Lock()
	budget, value, unlockErr := conn.unlockBudget, conn.unlockValue, conn.unlockCtxErr
	conn.mu.Unlock()
	if unlockErr != nil {
		t.Fatalf("release ran on an expired context: %v", unlockErr)
	}
	if budget < 4*time.Second || budget > 5*time.Second {
		t.Fatalf("release budget = %v, want about the full 5s shutdownReleaseBudget", budget)
	}
	if value != "caller" {
		t.Fatalf("release context value = %v, want the caller's value", value)
	}
}

func TestElection_ShutdownReleasesLockWithFreshDeadline(t *testing.T) {
	conn := newSession()
	s := storeOf(conn)
	e := newElection(t, s, testKey, singleTick)
	e.Start(context.Background())
	conn.locked.wait(t, 2*time.Second, "winning the lock")

	expired, cancel := context.WithCancel(context.Background())
	cancel()
	e.Shutdown(expired)

	attempted, unlocked, released, unlockErr := conn.state()
	if !attempted {
		t.Fatal("shutdown never attempted to release the advisory lock")
	}
	if unlockErr != nil {
		t.Fatalf("unlock ran on an already-expired context: %v", unlockErr)
	}
	if !unlocked {
		t.Fatal("advisory lock not released before the connection returned to the pool")
	}
	if !released {
		t.Fatal("connection was not returned to the pool")
	}
}

func TestElection_VerifyDoesNotFreezeOnStuckPing(t *testing.T) {
	conn := newSession()
	e := startElection(t, storeOf(conn), fastInterval)
	if !awaitLeader(e, 5*time.Second) {
		t.Fatal("election never settled into leadership")
	}

	conn.hang()

	conn.releasedAt.wait(t, 2*time.Second, "giving up on the unresponsive connection")
	if isLeader(e) {
		t.Fatal("the election kept leadership despite an unresponsive connection")
	}
	if got := e.Counters().HoldsLost; got != 1 {
		t.Fatalf("HoldsLost = %d, want 1", got)
	}
}

type blockingPingConn struct {
	pinging *signal
	unblock chan struct{}

	mu             sync.Mutex
	midPing        bool
	touchedMidPing bool
	unlocked       bool
	released       bool
}

func newBlockingPingConn() *blockingPingConn {
	return &blockingPingConn{pinging: newSignal(), unblock: make(chan struct{})}
}

func (c *blockingPingConn) TryAdvisoryLock(context.Context, int64) (bool, error) { return true, nil }

func (c *blockingPingConn) Ping(context.Context) error {
	c.setMidPing(true)
	defer c.setMidPing(false)
	c.pinging.fire()
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

func (c *blockingPingConn) touchedAtAll() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.unlocked || c.released
}

func TestElection_ShutdownReleasesOnlyAfterInFlightVerifyFinishes(t *testing.T) {
	conn := newBlockingPingConn()
	e := newElection(t, storeOf(conn), testKey, fastInterval)
	e.Start(context.Background())
	conn.pinging.wait(t, 2*time.Second, "the first verify ping")

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

type lateStore struct {
	conn      leader.LockSession
	acquiring *signal
	unblock   chan struct{}
}

func (s *lateStore) Acquire(context.Context) (leader.LockSession, error) {
	s.acquiring.fire()
	<-s.unblock
	return s.conn, nil
}

func TestElection_ShutdownReleasesALockWonWhileItWasRunning(t *testing.T) {
	conn := newSession()
	db := &lateStore{conn: conn, acquiring: newSignal(), unblock: make(chan struct{})}
	e := newElection(t, db, testKey, fastInterval)
	e.Start(context.Background())
	db.acquiring.wait(t, 2*time.Second, "the first lock attempt")

	budget, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	go func() { <-budget.Done(); close(db.unblock) }()
	e.Shutdown(budget)

	if _, unlocked, released, _ := conn.state(); !unlocked || !released {
		t.Fatal("a lock won while shutdown was under way was left held, with no loop left to release it")
	}
}

func TestElection_ShutdownKeepsTheLockWhenTheLoopNeverStops(t *testing.T) {
	logged := captureWarnings(t)
	conn := newBlockingPingConn()
	t.Cleanup(func() { close(conn.unblock) })
	e := newElection(t, storeOf(conn), testKey, fastInterval)
	e.Start(context.Background())
	conn.pinging.wait(t, 2*time.Second, "the verify ping that wedges the loop")

	budget, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	e.Shutdown(budget)

	if waited := time.Since(start); waited < shutdownLoopBudget {
		t.Fatalf("Shutdown gave up after %v, before the loop had its %v", waited, shutdownLoopBudget)
	}
	if conn.touchedAtAll() {
		t.Fatal("shutdown released a connection the loop goroutine may still be using")
	}
	if !strings.Contains(logged.String(), "leader.release_skipped") {
		t.Errorf("a retained lock was not reported to operators, got: %q", logged.String())
	}
}

func TestElection_WonLockSettlesBeforeLeadership(t *testing.T) {
	const interval = 200 * time.Millisecond
	conn := newSession()
	e := startElection(t, storeOf(conn), interval)
	conn.locked.wait(t, 2*time.Second, "winning the lock")
	won := time.Now()

	if _, _, ok := e.LeaderContext(context.Background()); ok {
		t.Fatal("LeaderContext issued inside the settle window")
	}
	if !awaitLeader(e, 5*time.Second) {
		t.Fatal("election never settled into leadership after winning the lock")
	}

	if settled := time.Since(won); settled <= predecessorDetectionIntervals*interval {
		t.Fatalf("leadership came %v after the lock, want longer than the predecessor's %v detection window", settled, predecessorDetectionIntervals*interval)
	}
	if !isLeader(e) {
		t.Fatal("a settled leader was refused a LeaderContext")
	}
}

var loggedSettle = regexp.MustCompile(`msg=leader\.lock_won .*settle=(\S+)`)

func TestElection_SettleWindowOutlastsPredecessorDetection(t *testing.T) {
	logged := captureLogs(t, slog.LevelInfo)
	e := newElection(t, storeOf(newSession()), testKey, fastInterval)
	e.Start(context.Background())
	if !awaitLeader(e, 5*time.Second) {
		t.Fatal("election never settled into leadership")
	}
	e.Shutdown(context.Background())

	m := loggedSettle.FindStringSubmatch(logged.String())
	if m == nil {
		t.Fatalf("no leader.lock_won record with a settle window in %q", logged.String())
	}
	settle, err := time.ParseDuration(m[1])
	if detection := predecessorDetectionIntervals * fastInterval; err != nil || settle <= detection {
		t.Fatalf("settle window = %q (%v), want longer than the predecessor's %v detection window", m[1], err, detection)
	}
}

func TestElection_SessionDeathCancelsInFlightLeaderContext(t *testing.T) {
	conn := newSession()
	e := startElection(t, storeOf(conn), fastInterval)
	if !awaitLeader(e, 5*time.Second) {
		t.Fatal("election never settled into leadership")
	}
	jobCtx, release, ok := e.LeaderContext(context.Background())
	if !ok {
		t.Fatal("settled leader was refused a LeaderContext")
	}
	defer release()

	conn.fail(errors.New("session terminated"))

	select {
	case <-jobCtx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight job context survived the loss of leadership")
	}
	if cause := context.Cause(jobCtx); !errors.Is(cause, leader.ErrLeadershipLost) {
		t.Fatalf("job context cause = %v, want ErrLeadershipLost", cause)
	}
	if _, _, ok := e.LeaderContext(context.Background()); ok {
		t.Fatal("a LeaderContext was issued after the term ended")
	}
}

func TestElection_ReleasedLeaderContextEndsOnlyThatJob(t *testing.T) {
	e := startElection(t, storeOf(newSession()), fastInterval)
	if !awaitLeader(e, 5*time.Second) {
		t.Fatal("election never settled into leadership")
	}

	jobCtx, release, ok := e.LeaderContext(context.Background())
	if !ok {
		t.Fatal("settled leader was refused a LeaderContext")
	}
	otherCtx, releaseOther, ok := e.LeaderContext(context.Background())
	if !ok {
		t.Fatal("settled leader was refused a second LeaderContext")
	}
	defer releaseOther()
	release()

	if !errors.Is(context.Cause(jobCtx), context.Canceled) {
		t.Fatalf("released context cause = %v, want context.Canceled", context.Cause(jobCtx))
	}
	if otherCtx.Err() != nil {
		t.Fatalf("releasing one job context ended another: %v", context.Cause(otherCtx))
	}
	if !isLeader(e) {
		t.Fatal("releasing one job context ended the whole term")
	}
}

func TestElection_SessionKilledMidJob_StaleWriteNotApplied(t *testing.T) {
	admin := dbtest.Pool(t)
	ctx := context.Background()
	mustExec(t, admin, "create table if not exists leader_fencing_1016 (writer text not null)")
	mustExec(t, admin, "truncate leader_fencing_1016")
	t.Cleanup(func() { _, _ = admin.Exec(ctx, "drop table if exists leader_fencing_1016") })

	a := fastElection(t, dbtest.Pool(t), databaseInterval)
	if !awaitLeader(a, 5*time.Second) {
		t.Fatal("instance A never became leader")
	}
	staleCtx, releaseStale, ok := a.LeaderContext(ctx)
	if !ok {
		t.Fatal("leader A was refused a LeaderContext")
	}
	defer releaseStale()

	terminateLockHolder(t, admin)

	b := fastElection(t, dbtest.Pool(t), databaseInterval)
	if !awaitLeader(b, 5*time.Second) {
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

	rows, err := admin.Query(ctx, "select writer from leader_fencing_1016 order by writer")
	if err != nil {
		t.Fatal(err)
	}
	writers, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
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
	polltest.Until(t, 2*time.Second, "the advisory lock to be released after its session was terminated", func() bool {
		var held bool
		err := admin.QueryRow(context.Background(), `
			select exists(select 1 from pg_locks
			where locktype = 'advisory' and granted and objsubid = 1
			  and ((classid::bigint << 32) | objid::bigint) = $1)`, testKey).Scan(&held)
		return err == nil && !held
	})
}

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
			conn := newSession()
			e := newElection(t, storeOf(conn), testKey, fastInterval)
			e.Start(context.Background())
			conn.locked.wait(t, 2*time.Second, "winning the lock")
			if tc.settled && !awaitLeader(e, 5*time.Second) {
				t.Fatal("election never settled into leadership")
			}

			conn.fail(errors.New("connection reset by peer"))
			conn.releasedAt.wait(t, 2*time.Second, "releasing the failed session")
			e.Shutdown(context.Background())

			if got := e.Counters().HoldsLost; got != 1 {
				t.Errorf("HoldsLost = %d, want 1", got)
			}
			if !strings.Contains(logged.String(), "leader.lost") || !strings.Contains(logged.String(), "connection reset by peer") {
				t.Errorf("leader.lost warning did not carry the ping error, got: %q", logged.String())
			}
		})
	}
}

func TestElection_ShutdownDuringVerifyReleasesLockWithoutLostWarning(t *testing.T) {
	const interval = 500 * time.Millisecond
	warnings := captureWarnings(t)
	conn := newSession()
	conn.pingWaitsForDeadline = true
	e := newElection(t, storeOf(conn), testKey, interval)
	e.Start(context.Background())
	conn.pinged.wait(t, 2*time.Second, "the first verify ping")

	e.Shutdown(context.Background())

	if _, unlocked, _, unlockErr := conn.state(); !unlocked {
		t.Fatalf("advisory lock not unlocked on a live context (unlock error: %v)", unlockErr)
	}
	if strings.Contains(warnings.String(), "leader.lost") {
		t.Fatalf("shutdown was logged as a lost lease: %s", warnings.String())
	}
	if got := e.Counters().HoldsLost; got != 0 {
		t.Fatalf("HoldsLost = %d, want 0: shutting down is not losing the lease", got)
	}
}
