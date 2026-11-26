package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type advisoryLocks struct{ pool *pgxpool.Pool }

func NewAdvisoryLocks(pool *pgxpool.Pool) *advisoryLocks { return &advisoryLocks{pool: pool} }

func (l *advisoryLocks) Acquire(ctx context.Context) (*advisorySession, error) {
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire a connection for the advisory lock: %w", err)
	}
	return &advisorySession{conn: conn}, nil
}

type advisorySession struct{ conn *pgxpool.Conn }

func (s *advisorySession) TryAdvisoryLock(ctx context.Context, key int64) (bool, error) {
	var won bool
	err := s.conn.QueryRow(ctx, "select pg_try_advisory_lock($1)", key).Scan(&won)
	if err != nil {
		return won, fmt.Errorf("try advisory lock %d: %w", key, err)
	}
	return won, nil
}

func (s *advisorySession) Ping(ctx context.Context) error {
	if err := s.conn.Ping(ctx); err != nil {
		return fmt.Errorf("ping the lock connection: %w", err)
	}
	return nil
}

func (s *advisorySession) AdvisoryUnlock(ctx context.Context, key int64) error {
	_, err := s.conn.Exec(ctx, "select pg_advisory_unlock($1)", key)
	if err != nil {
		return fmt.Errorf("advisory unlock %d: %w", key, err)
	}
	return nil
}

func (s *advisorySession) Release() { s.conn.Release() }
