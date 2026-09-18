package leader

import (
	"altune/go-api/internal/shared/runloop"
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultInterval = 10 * time.Second

	// settleIntervals is how many election intervals a freshly won lock must be
	// held (and re-verified) before this instance's leadership term begins. A
	// previous leader whose DB session died notices within two intervals (one to
	// reach its next verify tick, one for that verify's bounded Ping to fail) and
	// ends its term, canceling every job running under LeaderContext. Postgres
	// frees the lock no earlier than that session's death, so settling for longer
	// than two intervals after winning it keeps the stale term and the new one
	// from overlapping. 2.5 lets the third verify tick after the win settle
	// despite ticker jitter.
	settleIntervals = 2.5

	// shutdownReleaseBudget bounds the advisory-unlock performed on shutdown.
	// release runs under its OWN fresh context (never the app's per-component
	// budget, which the wait phase may already have exhausted), so a drained
	// shutdown deadline can never silently skip returning the lock.
	shutdownReleaseBudget = 5 * time.Second

	// shutdownLoopBudget bounds the wait for the loop goroutine to leave a tick
	// that shutdown has just canceled. Like the release budget it is fresh rather
	// than the caller's, which runloop.Background may already have spent waiting.
	// Every tick operation is bounded by the loop context, so an honest driver
	// unwinds at once: this is slack for a wedged one, not an expected wait.
	shutdownLoopBudget = 2 * time.Second
)

// ErrLeadershipLost is the cancellation cause of every context issued by
// LeaderContext once the term it was issued under ends.
var ErrLeadershipLost = errors.New("leader: leadership lost")

// connector hands out connections. *pgxpool.Pool satisfies it in production;
// tests inject a fake to drive the timing behaviour without a live database.
type connector interface {
	Acquire(ctx context.Context) (heldConn, error)
}

// heldConn is the subset of a pooled connection the election needs. It owns the
// advisory lock for as long as the election holds it.
type heldConn interface {
	TryAdvisoryLock(ctx context.Context, key int64) (bool, error)
	Ping(ctx context.Context) error
	AdvisoryUnlock(ctx context.Context, key int64) error
	Release()
}

type Election struct {
	db       connector
	key      int64
	interval time.Duration

	mu        sync.RWMutex
	conn      heldConn
	heldSince time.Time
	// term is the current leadership tenure, ended the moment the lock is
	// detached (failed verify or shutdown). It is nil while not leader,
	// including inside the settle window.
	term *term

	counters electionCounters

	won  chan struct{}
	once sync.Once

	// loopDone closes when the loop goroutine has returned. runloop.Background's
	// own done signal is only ever waited on for as long as the caller's budget
	// allows; this one is what shutdown blocks on before it may touch conn.
	loopDone chan struct{}
	runloop.Background
}

func NewElection(pool *pgxpool.Pool, key int64) *Election {
	return &Election{
		db:       pgxConnector{pool: pool},
		key:      key,
		interval: defaultInterval,
		won:      make(chan struct{}),
	}
}

// IsLeader reports whether this instance holds the lock AND its term has
// settled. A lock still inside its settle window does not count yet: a stale
// leader may not have cut its jobs off.
func (e *Election) IsLeader() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.term != nil
}

// Counters reports what this instance's elections have done so far. It is the
// history IsLeader lacks: a false there means "not leader", never why.
func (e *Election) Counters() Counters {
	return e.counters.read()
}

// LeaderContext returns a child of parent that is also canceled, with cause
// ErrLeadershipLost, as soon as the current leadership term ends. Leader-only
// work must run under it so a leader whose DB session dies mid-job is cut off
// instead of racing its successor's run of the same job. ok is false (and ctx
// and release nil) when this instance is not currently leader. The caller must
// call release once the work is done.
func (e *Election) LeaderContext(parent context.Context) (ctx context.Context, release context.CancelFunc, ok bool) {
	e.mu.RLock()
	current := e.term
	e.mu.RUnlock()
	if current == nil {
		return nil, nil, false
	}
	return current.issue(parent)
}

func (e *Election) Await(ctx context.Context) bool {
	select {
	case <-e.won:
		return true
	case <-ctx.Done():
		return false
	}
}

func (e *Election) Start(ctx context.Context) {
	// done is captured, not read back off e, so each spawned loop closes its own
	// signal and no second Start can make two goroutines close one channel.
	done := make(chan struct{})
	e.loopDone = done
	e.Spawn(ctx, func(loopCtx context.Context) {
		defer close(done)
		e.loop(loopCtx)
	})
}

func (e *Election) loop(ctx context.Context) {
	e.tick(ctx)
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.tick(ctx)
		}
	}
}

func (e *Election) tick(ctx context.Context) {
	if e.holding() {
		e.verify(ctx)
		return
	}
	e.acquire(ctx)
}

// holding reports whether the advisory lock is held, settled or not.
func (e *Election) holding() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.conn != nil
}

// opCtx bounds a single tick operation. Derived from the tick interval, it caps
// how long a wedged Postgres call (a partition with no RST) can block the
// single-goroutine loop, so a stuck connection can't freeze leadership: the op
// is abandoned within one interval and the next tick still fires.
func (e *Election) opCtx(base context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(base, e.interval)
}

func (e *Election) acquire(base context.Context) {
	ctx, cancel := e.opCtx(base)
	defer cancel()
	e.counters.attempts.Add(1)
	conn, won := e.lockedConn(ctx)
	if !won {
		return
	}
	e.hold(conn)
	e.counters.wins.Add(1)
	slog.InfoContext(ctx, "leader.lock_won", "key", e.key, "settle", e.settleWindow().String())
}

// lockedConn returns a connection that holds the advisory lock. It keeps a real
// database failure apart from losing the race to another instance: the latter is
// the expected outcome of every standby's tick, while the former means this
// instance is not in the election at all and nothing else would say so.
func (e *Election) lockedConn(ctx context.Context) (heldConn, bool) {
	conn, err := e.db.Acquire(ctx)
	if err != nil {
		e.acquireFailed(ctx, "pool_acquire", err)
		return nil, false
	}
	switch won, err := conn.TryAdvisoryLock(ctx, e.key); {
	case err != nil:
		e.acquireFailed(ctx, "advisory_lock", err)
	case !won:
		e.counters.contended.Add(1)
		slog.DebugContext(ctx, "leader.lock_contended", "key", e.key)
	default:
		return conn, true
	}
	conn.Release()
	return nil, false
}

// acquireFailed records a failure to take the lock that no rival caused. It is
// warn, not info: an instance failing here will never lead, and every other
// signal (IsLeader, the absence of leader.lock_won) looks like a healthy standby.
func (e *Election) acquireFailed(ctx context.Context, stage string, err error) {
	e.counters.failures.Add(1)
	slog.WarnContext(ctx, "leader.acquire_failed", "key", e.key, "stage", stage, "err", err)
}

func (e *Election) hold(conn heldConn) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.conn = conn
	e.heldSince = time.Now()
}

// settleWindow is how long a won lock must be held before its term begins.
func (e *Election) settleWindow() time.Duration {
	return time.Duration(settleIntervals * float64(e.interval))
}

// settle begins the leadership term once the lock has been held, and just
// re-verified, for the whole settle window.
func (e *Election) settle(ctx context.Context) {
	e.mu.Lock()
	if e.conn == nil || e.term != nil || time.Since(e.heldSince) < e.settleWindow() {
		e.mu.Unlock()
		return
	}
	e.term = newTerm()
	e.mu.Unlock()
	e.once.Do(func() { close(e.won) })
	slog.InfoContext(ctx, "leader.acquired", "key", e.key)
}

func (e *Election) verify(base context.Context) {
	e.mu.RLock()
	conn := e.conn
	e.mu.RUnlock()
	if conn == nil {
		return
	}
	ctx, cancel := e.opCtx(base)
	defer cancel()
	if conn.Ping(ctx) == nil {
		e.settle(base)
		return
	}
	slog.WarnContext(base, "leader.lost", "key", e.key)
	e.release(base)
}

// release returns the advisory lock. base supplies the deadline; callers pass a
// context they know is still live (shutdown uses a fresh one) so the unlock is
// never issued on an already-expired context. A failed unlock is logged, not
// discarded, since the connection is about to be returned to the pool.
//
// Caller must own the connection: this hands it back to the pool, so it may only
// be called from the loop goroutine or from a shutdown that has outlived it.
func (e *Election) release(base context.Context) {
	conn := e.detach()
	if conn == nil {
		return
	}
	ctx, cancel := e.opCtx(base)
	defer cancel()
	if err := conn.AdvisoryUnlock(ctx, e.key); err != nil {
		slog.WarnContext(ctx, "leader.unlock_failed", "key", e.key, "err", err)
	}
	conn.Release()
}

// detach drops the held connection and ends the current term, canceling every
// LeaderContext issued under it before the lock is unlocked.
func (e *Election) detach() heldConn {
	e.mu.Lock()
	defer e.mu.Unlock()
	conn := e.conn
	e.conn = nil
	if e.term != nil {
		e.term.end()
		e.counters.termEnds.Add(1)
	}
	e.term = nil
	return conn
}

func (e *Election) Shutdown(ctx context.Context) {
	e.Background.Shutdown(ctx)
	if !e.loopExited(shutdownLoopBudget) {
		slog.WarnContext(ctx, "leader.release_skipped", "key", e.key, "waited", shutdownLoopBudget.String())
		return
	}
	releaseCtx, cancel := context.WithTimeout(context.Background(), shutdownReleaseBudget)
	defer cancel()
	e.release(releaseCtx)
}

// loopExited reports whether the loop goroutine is gone, waiting up to within
// for it. A tick still in flight owns the held connection, so releasing before
// this holds hands a connection back to the pool that a Ping is still using; the
// lock is better left to clear with this instance's DB session than unlocked and
// returned underneath a live caller. A nil channel means no loop ever ran.
func (e *Election) loopExited(within time.Duration) bool {
	if e.loopDone == nil {
		return true
	}
	select {
	case <-e.loopDone:
		return true
	case <-time.After(within):
		return false
	}
}
