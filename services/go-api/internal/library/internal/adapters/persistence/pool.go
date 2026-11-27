package persistence

import (
	"altune/go-api/internal/library/internal/core/ports"
	"altune/go-api/internal/shared/database"
	"context"
	"errors"
	"fmt"
	"time"
)

const dbCallTimeout = 5 * time.Second

var errDBCallTimeout = errors.New("catalog db call exceeded dbCallTimeout")

var ErrDBCallMetricsRequired = errors.New("persistence: the repository requires db call metrics")

func requireDBCallMetrics(name string, m ports.DBCallMetrics) {
	if m == nil {
		panic(name + ": " + ErrDBCallMetricsRequired.Error())
	}
}

func withDBTimeout(ctx context.Context, metrics ports.DBCallMetrics) (context.Context, context.CancelFunc) {
	dbCtx, cancel := context.WithTimeoutCause(ctx, dbCallTimeout, errDBCallTimeout)
	return dbCtx, func() {
		recordIfDBCallTimedOut(dbCtx, metrics)
		cancel()
	}
}

func classifyDBError(err error) error {
	if errors.Is(err, ports.ErrDBTransient) || !database.IsTransient(err) {
		return err
	}
	return fmt.Errorf("%w: %w", ports.ErrDBTransient, err)
}

func recordIfDBCallTimedOut(dbCtx context.Context, metrics ports.DBCallMetrics) {
	if errors.Is(context.Cause(dbCtx), errDBCallTimeout) {
		metrics.DBCallTimedOut()
	}
}
