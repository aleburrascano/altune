package persistence

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// pgxPool is the subset of *pgxpool.Pool the catalog persistence adapters use.
// Depending on this interface rather than the concrete pool lets a test inject a
// store that blocks forever, proving that withDBTimeout bounds a stuck call. A
// real *pgxpool.Pool satisfies it, so the public constructors are unchanged.
type pgxPool interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// dbCallTimeout bounds a single logical database operation (one query, or one
// transaction from Begin to Commit). It is derived from the caller's context so
// a shorter caller deadline still wins; its purpose is to cap the worst case so
// a wedged pgx call — and the pooled connection it holds — is released after at
// most this long rather than blocking the handler goroutine indefinitely and
// starving every other request contending for the bounded pool.
//
// A var, not a const, so a test can shrink it to keep a bounded-call assertion
// fast. Five seconds is a deliberate default, not a tuned one (see issue #426).
var dbCallTimeout = 5 * time.Second

// withDBTimeout derives a child context bounded by dbCallTimeout from ctx. The
// caller MUST defer the returned cancel to release the timer. Because it derives
// from ctx, a caller that already carries a shorter deadline keeps it.
func withDBTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, dbCallTimeout)
}
