package leader

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresConnector struct{ pool *pgxpool.Pool }

func PostgresLocks(pool *pgxpool.Pool) LockStore { return postgresConnector{pool: pool} }

func (c postgresConnector) Acquire(ctx context.Context) (LockSession, error) {
	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire a connection for the leader lock: %w", err)
	}
	return postgresHeldConn{conn: conn}, nil
}

type postgresHeldConn struct{ conn *pgxpool.Conn }

func (c postgresHeldConn) TryAdvisoryLock(ctx context.Context, key int64) (bool, error) {
	var won bool
	err := c.conn.QueryRow(ctx, "select pg_try_advisory_lock($1)", key).Scan(&won)
	if err != nil {
		return won, fmt.Errorf("try advisory lock %d: %w", key, err)
	}
	return won, nil
}

func (c postgresHeldConn) Ping(ctx context.Context) error {
	if err := c.conn.Ping(ctx); err != nil {
		return fmt.Errorf("ping the leader connection: %w", err)
	}
	return nil
}

func (c postgresHeldConn) AdvisoryUnlock(ctx context.Context, key int64) error {
	_, err := c.conn.Exec(ctx, "select pg_advisory_unlock($1)", key)
	if err != nil {
		return fmt.Errorf("advisory unlock %d: %w", key, err)
	}
	return nil
}

func (c postgresHeldConn) Release() { c.conn.Release() }
