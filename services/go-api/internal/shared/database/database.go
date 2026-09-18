package database

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrUnreachable reports that the connect+ping phase of NewPool did not
// finish within connectTimeout.
var ErrUnreachable = errors.New("database unreachable")

// connectTimeout bounds NewPool's connect+ping phase regardless of the
// caller's context, so a black-holed host fails startup fast instead of
// hanging before the listener binds. A var so tests can shorten it.
var connectTimeout = 10 * time.Second

// defaultMaxConns is the pool ceiling applied when the caller passes a
// non-positive DB_POOL_MAX_CONNS. pgx's own default is max(4, NumCPU), which
// makes the ceiling a property of the container shape — 4 on a 2-vCPU host,
// fewer than the acquisition workers alone hold — so the fallback is a fixed
// number instead, and a misconfiguration can never mean "whatever the host
// implies".
const defaultMaxConns = 20

func NewPool(ctx context.Context, databaseURL string, maxConns int) (*pgxpool.Pool, error) {
	cfg, err := poolConfig(databaseURL, maxConns)
	if err != nil {
		return nil, err
	}

	// NewWithConfig dials lazily and hands ctx to its background min-conns
	// warmup, so it keeps the caller's ctx; the first real dial happens in
	// Ping, which is what the deadline bounds.
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	timeout := connectTimeout
	connectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := pool.Ping(connectCtx); err != nil {
		pool.Close()
		if errors.Is(connectCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			return nil, fmt.Errorf("%w within %s: %w", ErrUnreachable, timeout, err)
		}
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}

func poolConfig(databaseURL string, maxConns int) (*pgxpool.Config, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL not set")
	}

	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = poolCeiling(maxConns)
	return cfg, nil
}

// poolCeiling narrows a configured connection count to what pgx accepts. A
// value outside int32 is treated like a non-positive one: unusable input gets
// the documented default rather than the negative ceiling the conversion would
// otherwise produce.
func poolCeiling(maxConns int) int32 {
	if maxConns <= 0 || maxConns > math.MaxInt32 {
		return defaultMaxConns
	}
	return int32(maxConns)
}

// PoolStats is a point-in-time read of the pool's saturation. EmptyAcquireCount
// is the saturation signal: it advances whenever an acquire found no idle
// connection and had to wait — expected a few times while a cold pool grows,
// but climbing steadily once TotalConns has reached MaxConns means the ceiling,
// not the database, is what callers are queueing behind.
type PoolStats struct {
	AcquiredConns     int32 `json:"acquired_conns"`
	TotalConns        int32 `json:"total_conns"`
	MaxConns          int32 `json:"max_conns"`
	EmptyAcquireCount int64 `json:"empty_acquire_count"`
}

// ReadPoolStats reads the counters without blocking acquisition, so the fields
// can be marginally inconsistent with each other — acceptable for an operator
// view. A nil pool reads as a zero value.
func ReadPoolStats(pool *pgxpool.Pool) PoolStats {
	if pool == nil {
		return PoolStats{}
	}
	stat := pool.Stat()
	return PoolStats{
		AcquiredConns:     stat.AcquiredConns(),
		TotalConns:        stat.TotalConns(),
		MaxConns:          stat.MaxConns(),
		EmptyAcquireCount: stat.EmptyAcquireCount(),
	}
}

type HealthStatus struct {
	OK  bool
	Err error
}

func CheckHealth(ctx context.Context, pool *pgxpool.Pool) HealthStatus {
	err := pool.Ping(ctx)
	return HealthStatus{OK: err == nil, Err: err}
}
