package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	if databaseURL == "" {
		return nil, nil
	}

	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}

type HealthStatus struct {
	OK  bool
	Err error
}

func CheckHealth(ctx context.Context, pool *pgxpool.Pool) HealthStatus {
	if pool == nil {
		return HealthStatus{OK: false, Err: fmt.Errorf("not configured")}
	}
	err := pool.Ping(ctx)
	return HealthStatus{OK: err == nil, Err: err}
}
