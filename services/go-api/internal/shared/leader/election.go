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

	won  chan struct{}
	once sync.Once
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
	e.Spawn(ctx, e.loop)
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
	conn, err := e.db.Acquire(ctx)
	if err != nil {
		return
	}
	won, err := conn.TryAdvisoryLock(ctx, e.key)
	if err != nil || !won {
		conn.Release()
		return
	}
	e.mu.Lock()
	e.conn = conn
	e.heldSince = time.Now()
	e.mu.Unlock()
	slog.InfoContext(ctx, "leader.lock_won", "key", e.key, "settle", e.settleWindow().String())
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
	}
	e.term = nil
	return conn
}

func (e *Election) Shutdown(ctx context.Context) {
	e.Background.Shutdown(ctx)
	releaseCtx, cancel := context.WithTimeout(context.Background(), shutdownReleaseBudget)
	defer cancel()
	e.release(releaseCtx)
}
