package database

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrUnreachable = errors.New("database unreachable")

var connectTimeout = 10 * time.Second

const defaultMaxConns = 20

func NewPool(ctx context.Context, databaseURL string, maxConns int) (*pgxpool.Pool, error) {
	cfg, err := poolConfig(databaseURL, maxConns)
	if err != nil {
		return nil, err
	}

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

func poolCeiling(maxConns int) int32 {
	if maxConns <= 0 || maxConns > math.MaxInt32 {
		return defaultMaxConns
	}
	return int32(maxConns)
}

type PoolStats struct {
	AcquiredConns     int32 `json:"acquired_conns"`
	TotalConns        int32 `json:"total_conns"`
	MaxConns          int32 `json:"max_conns"`
	EmptyAcquireCount int64 `json:"empty_acquire_count"`
}

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
