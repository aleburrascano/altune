package database

import (
	"context"
	"errors"
	"fmt"
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

func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL not set")
	}

	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
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

type HealthStatus struct {
	OK  bool
	Err error
}

func CheckHealth(ctx context.Context, pool *pgxpool.Pool) HealthStatus {
	err := pool.Ping(ctx)
	return HealthStatus{OK: err == nil, Err: err}
}
