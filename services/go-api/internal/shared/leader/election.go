package leader

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"altune/go-api/internal/shared/runloop"
)

const (
	defaultInterval = 10 * time.Second

	// shutdownReleaseBudget bounds the advisory-unlock performed on shutdown.
	// release runs under its OWN fresh context (never the app's per-component
	// budget, which the wait phase may already have exhausted), so a drained
	// shutdown deadline can never silently skip returning the lock.
	shutdownReleaseBudget = 5 * time.Second
)

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

	mu   sync.RWMutex
	conn heldConn

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

func (e *Election) IsLeader() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.conn != nil
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
	if e.IsLeader() {
		e.verify(ctx)
		return
	}
	e.acquire(ctx)
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

func (e *Election) detach() heldConn {
	e.mu.Lock()
	defer e.mu.Unlock()
	conn := e.conn
	e.conn = nil
	return conn
}

func (e *Election) Shutdown(ctx context.Context) {
	e.Background.Shutdown(ctx)
	releaseCtx, cancel := context.WithTimeout(context.Background(), shutdownReleaseBudget)
	defer cancel()
	e.release(releaseCtx)
}

// pgxConnector adapts *pgxpool.Pool to connector.
type pgxConnector struct{ pool *pgxpool.Pool }

func (c pgxConnector) Acquire(ctx context.Context) (heldConn, error) {
	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	return pgxHeldConn{conn: conn}, nil
}

// pgxHeldConn adapts a pooled *pgxpool.Conn to heldConn.
type pgxHeldConn struct{ conn *pgxpool.Conn }

func (c pgxHeldConn) TryAdvisoryLock(ctx context.Context, key int64) (bool, error) {
	var won bool
	err := c.conn.QueryRow(ctx, "select pg_try_advisory_lock($1)", key).Scan(&won)
	return won, err
}

func (c pgxHeldConn) Ping(ctx context.Context) error { return c.conn.Ping(ctx) }

func (c pgxHeldConn) AdvisoryUnlock(ctx context.Context, key int64) error {
	_, err := c.conn.Exec(ctx, "select pg_advisory_unlock($1)", key)
	return err
}

func (c pgxHeldConn) Release() { c.conn.Release() }
