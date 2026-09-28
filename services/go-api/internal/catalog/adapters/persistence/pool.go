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

type pgxPool interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

var dbCallTimeout = 5 * time.Second

var errDBCallTimeout = errors.New("catalog db call exceeded dbCallTimeout")

var dbCallMetrics atomic.Pointer[ports.DBCallMetrics]

func SetDBCallMetrics(m ports.DBCallMetrics) {
	if m == nil {
		m = ports.NoopDBCallMetrics()
	}
	dbCallMetrics.Store(&m)
}

func withDBTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	dbCtx, cancel := context.WithTimeoutCause(ctx, dbCallTimeout, errDBCallTimeout)
	var once sync.Once
	return dbCtx, func() {
		once.Do(func() { recordIfDBCallTimedOut(dbCtx) })
		cancel()
	}
}

func classifyDBError(err error) error {
	if err == nil || errors.Is(err, ports.ErrDBTransient) || !isTransientDBError(err) {
		return err
	}
	return fmt.Errorf("%w: %w", ports.ErrDBTransient, err)
}

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

func isTransientSQLState(code string) bool {
	if strings.HasPrefix(code, "08") || strings.HasPrefix(code, "53") {
		return true
	}
	switch code {
	case "57P01",
		"57P02",
		"57P03",
		"40001",
		"40P01":
		return true
	}
	return false
}

func recordIfDBCallTimedOut(dbCtx context.Context) {
	if !errors.Is(context.Cause(dbCtx), errDBCallTimeout) {
		return
	}
	if m := dbCallMetrics.Load(); m != nil {
		(*m).DBCallTimedOut()
	}
}
