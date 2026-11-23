package persistence

import (
	"altune/go-api/internal/catalog/internal/core/ports"
	"altune/go-api/internal/shared/database"
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"
)

const dbCallTimeout = 5 * time.Second

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
	return dbCtx, func() {
		recordIfDBCallTimedOut(dbCtx)
		cancel()
	}
}

func classifyDBError(err error) error {
	if errors.Is(err, ports.ErrDBTransient) || !database.IsTransient(err) {
		return err
	}
	return fmt.Errorf("%w: %w", ports.ErrDBTransient, err)
}

func recordIfDBCallTimedOut(dbCtx context.Context) {
	if !errors.Is(context.Cause(dbCtx), errDBCallTimeout) {
		return
	}
	if m := dbCallMetrics.Load(); m != nil {
		(*m).DBCallTimedOut()
	}
}
