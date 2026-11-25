package leader

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type pgxConnector struct{ pool *pgxpool.Pool }

func PoolLocks(pool *pgxpool.Pool) LockStore { return pgxConnector{pool: pool} }

func (c pgxConnector) Acquire(ctx context.Context) (LockSession, error) {
	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire a connection for the leader lock: %w", err)
	}
	return pgxHeldConn{conn: conn}, nil
}

type pgxHeldConn struct{ conn *pgxpool.Conn }

func (c pgxHeldConn) TryAdvisoryLock(ctx context.Context, key int64) (bool, error) {
	var won bool
	err := c.conn.QueryRow(ctx, "select pg_try_advisory_lock($1)", key).Scan(&won)
	if err != nil {
		return won, fmt.Errorf("try advisory lock %d: %w", key, err)
	}
	return won, nil
}

func (c pgxHeldConn) Ping(ctx context.Context) error {
	if err := c.conn.Ping(ctx); err != nil {
		return fmt.Errorf("ping the leader connection: %w", err)
	}
	return nil
}

func (c pgxHeldConn) AdvisoryUnlock(ctx context.Context, key int64) error {
	_, err := c.conn.Exec(ctx, "select pg_advisory_unlock($1)", key)
	if err != nil {
		return fmt.Errorf("advisory unlock %d: %w", key, err)
	}
	return nil
}

func (c pgxHeldConn) Release() { c.conn.Release() }
