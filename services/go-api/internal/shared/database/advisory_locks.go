package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type AdvisoryLocks struct{ pool *pgxpool.Pool }

func NewAdvisoryLocks(pool *pgxpool.Pool) *AdvisoryLocks { return &AdvisoryLocks{pool: pool} }

func (l *AdvisoryLocks) Acquire(ctx context.Context) (*LockSession, error) {
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire a connection for the advisory lock: %w", err)
	}
	return &LockSession{conn: conn}, nil
}

type LockSession struct{ conn *pgxpool.Conn }

func (s *LockSession) TryAdvisoryLock(ctx context.Context, key int64) (bool, error) {
	var won bool
	err := s.conn.QueryRow(ctx, "select pg_try_advisory_lock($1)", key).Scan(&won)
	if err != nil {
		return won, fmt.Errorf("try advisory lock %d: %w", key, err)
	}
	return won, nil
}

func (s *LockSession) Ping(ctx context.Context) error {
	if err := s.conn.Ping(ctx); err != nil {
		return fmt.Errorf("ping the lock connection: %w", err)
	}
	return nil
}

func (s *LockSession) AdvisoryUnlock(ctx context.Context, key int64) error {
	_, err := s.conn.Exec(ctx, "select pg_advisory_unlock($1)", key)
	if err != nil {
		return fmt.Errorf("advisory unlock %d: %w", key, err)
	}
	return nil
}

func (s *LockSession) Release() { s.conn.Release() }
