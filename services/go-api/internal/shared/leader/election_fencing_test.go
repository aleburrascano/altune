package leader

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

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

	time.Sleep(e.settleWindow())
	e.tick(context.Background())
	if !e.IsLeader() {
		t.Fatal("election never settled into leadership after the settle window")
	}
}

// TestElection_SessionDeathCancelsInFlightLeaderContext is the #1016 unit
// regression: a job running under LeaderContext must be canceled, with cause
// ErrLeadershipLost, as soon as the election detects its session died, rather
// than running on to completion alongside the next leader.
func TestElection_SessionDeathCancelsInFlightLeaderContext(t *testing.T) {
	fc := &flakyConn{}
	e := &Election{db: fixedConnector{fc}, key: testKey, interval: time.Millisecond, won: make(chan struct{})}
	e.tick(context.Background())
	time.Sleep(e.settleWindow())
	e.tick(context.Background())

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
	e.tick(context.Background())
	time.Sleep(e.settleWindow())
	e.tick(context.Background())

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
