package persistence

import (
	"altune/go-api/internal/catalog/ports"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
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

// errDBCallTimeout is the cancellation cause withDBTimeout attaches to its own
// deadline, so the returned cancel can tell "our bound fired" apart from the
// caller's context being cancelled or reaching its own (earlier) deadline.
var errDBCallTimeout = errors.New("catalog db call exceeded dbCallTimeout")

// dbCallMetrics receives one DBCallTimedOut per operation cut off by
// dbCallTimeout. It is process-wide because withDBTimeout is shared by every
// catalog repository; the app wires the real counter via SetDBCallMetrics.
var dbCallMetrics atomic.Pointer[ports.DBCallMetrics]

// SetDBCallMetrics installs the counter the catalog persistence adapters report
// DB-call timeouts to. A nil m restores the no-op default.
func SetDBCallMetrics(m ports.DBCallMetrics) {
	if m == nil {
		m = ports.NoopDBCallMetrics()
	}
	dbCallMetrics.Store(&m)
}

// withDBTimeout derives a child context bounded by dbCallTimeout from ctx. The
// caller MUST defer the returned cancel to release the timer. Because it derives
// from ctx, a caller that already carries a shorter deadline keeps it. Calling
// cancel after dbCallTimeout (not the caller) ended the operation records one
// DB-call timeout; repeated cancel calls record it at most once.
func withDBTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	dbCtx, cancel := context.WithTimeoutCause(ctx, dbCallTimeout, errDBCallTimeout)
	var once sync.Once
	return dbCtx, func() {
		once.Do(func() { recordIfDBCallTimedOut(dbCtx) })
		cancel()
	}
}

// classifyDBError marks err with ports.ErrDBTransient when it is a failure a
// retry may clear, and returns it unchanged otherwise (nil stays nil). The
// original error stays in the chain, so pgx.ErrNoRows and friends still match.
func classifyDBError(err error) error {
	if err == nil || errors.Is(err, ports.ErrDBTransient) || !isTransientDBError(err) {
		return err
	}
	return fmt.Errorf("%w: %w", ports.ErrDBTransient, err)
}

// isTransientDBError reports whether err is a deadline (the withDBTimeout guard
// or an upstream one), a lost or unreachable connection, or a server error whose
// SQLSTATE names a connection, resource, shutdown or concurrency condition. A
// server error is judged by its SQLSTATE alone, so a connect attempt the server
// rejected for a permanent reason (bad password, unknown database) stays
// permanent even though it also arrives wrapped in a *pgconn.ConnectError.
// Caller cancellation is not transient: nobody is waiting to retry.
func isTransientDBError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return isTransientSQLState(pgErr.Code)
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || pgconn.Timeout(err) || pgconn.SafeToRetry(err) {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var connectErr *pgconn.ConnectError
	if errors.As(err, &connectErr) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

// isTransientSQLState reports whether a SQLSTATE names a condition that can
// clear on retry: class 08 (connection exception), class 53 (insufficient
// resources), the 57P0x shutdown/startup states, and serialization failure or
// deadlock.
func isTransientSQLState(code string) bool {
	if strings.HasPrefix(code, "08") || strings.HasPrefix(code, "53") {
		return true
	}
	switch code {
	case "57P01", // admin_shutdown
		"57P02", // crash_shutdown
		"57P03", // cannot_connect_now
		"40001", // serialization_failure
		"40P01": // deadlock_detected
		return true
	}
	return false
}

// recordIfDBCallTimedOut counts dbCtx as a DB-call timeout only when its own
// dbCallTimeout deadline is what ended it.
func recordIfDBCallTimedOut(dbCtx context.Context) {
	if !errors.Is(context.Cause(dbCtx), errDBCallTimeout) {
		return
	}
	if m := dbCallMetrics.Load(); m != nil {
		(*m).DBCallTimedOut()
	}
}
