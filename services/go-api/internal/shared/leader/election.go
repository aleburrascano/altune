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

	settleIntervals = 2.5

	shutdownReleaseBudget = 5 * time.Second

	shutdownLoopBudget = 2 * time.Second
)

var ErrLeadershipLost = errors.New("leader: leadership lost")

type connector interface {
	Acquire(ctx context.Context) (heldConn, error)
}

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
	term      *term

	counters electionCounters

	won  chan struct{}
	once sync.Once

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

func (e *Election) isLeader() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.term != nil
}

func (e *Election) Counters() Counters {
	return e.counters.read()
}

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

func (e *Election) holding() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.conn != nil
}

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

func (e *Election) settleWindow() time.Duration {
	return time.Duration(settleIntervals * float64(e.interval))
}

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
	err := conn.Ping(ctx)
	if err == nil {
		e.settle(base)
		return
	}
	slog.WarnContext(base, "leader.lost", "key", e.key, "err", err)
	e.counters.holdsLost.Add(1)
	e.release(base)
}

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
