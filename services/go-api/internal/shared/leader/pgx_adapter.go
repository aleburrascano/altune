package leader

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// pgxConnector adapts *pgxpool.Pool to connector.
type pgxConnector struct{ pool *pgxpool.Pool }

func (c pgxConnector) Acquire(ctx context.Context) (heldConn, error) {
	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	return pgxHeldConn{conn: conn}, nil
}

// pgxHeldConn adapts a pooled *pgxpool.Conn to heldConn.
type pgxHeldConn struct{ conn *pgxpool.Conn }

func (c pgxHeldConn) TryAdvisoryLock(ctx context.Context, key int64) (bool, error) {
	var won bool
	err := c.conn.QueryRow(ctx, "select pg_try_advisory_lock($1)", key).Scan(&won)
	return won, err
}

func (c pgxHeldConn) Ping(ctx context.Context) error { return c.conn.Ping(ctx) }

func (c pgxHeldConn) AdvisoryUnlock(ctx context.Context, key int64) error {
	_, err := c.conn.Exec(ctx, "select pg_advisory_unlock($1)", key)
	return err
}

func (c pgxHeldConn) Release() { c.conn.Release() }
