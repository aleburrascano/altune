package leader

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"altune/go-api/internal/shared/runloop"
)

const defaultInterval = 10 * time.Second

type Election struct {
	pool     *pgxpool.Pool
	key      int64
	interval time.Duration

	mu   sync.RWMutex
	conn *pgxpool.Conn

	won  chan struct{}
	once sync.Once
	runloop.Background
}

func NewElection(pool *pgxpool.Pool, key int64) *Election {
	return &Election{
		pool:     pool,
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

func (e *Election) acquire(ctx context.Context) {
	conn, err := e.pool.Acquire(ctx)
	if err != nil {
		return
	}
	var won bool
	err = conn.QueryRow(ctx, "select pg_try_advisory_lock($1)", e.key).Scan(&won)
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

func (e *Election) verify(ctx context.Context) {
	e.mu.RLock()
	conn := e.conn
	e.mu.RUnlock()
	if conn == nil || conn.Ping(ctx) == nil {
		return
	}
	slog.WarnContext(ctx, "leader.lost", "key", e.key)
	e.release(ctx)
}

func (e *Election) release(ctx context.Context) {
	e.mu.Lock()
	conn := e.conn
	e.conn = nil
	e.mu.Unlock()
	if conn == nil {
		return
	}
	_, _ = conn.Exec(ctx, "select pg_advisory_unlock($1)", e.key)
	conn.Release()
}

func (e *Election) Shutdown(ctx context.Context) {
	e.Background.Shutdown(ctx)
	e.release(ctx)
}
