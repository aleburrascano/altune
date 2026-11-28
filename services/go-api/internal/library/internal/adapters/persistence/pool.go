package persistence

import (
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/library/internal/ports"
	"altune/go-api/internal/shared/database"
	"context"
	"errors"
	"fmt"
	"time"
)

const dbCallTimeout = 5 * time.Second

var errDBCallTimeout = errors.New("catalog db call exceeded dbCallTimeout")

func requireDBCallMetrics(name string, m ports.DBCallMetrics) {
	if m == nil {
		panic(name + ": " + domain.ErrDBCallMetricsRequired.Error())
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
	if errors.Is(err, domain.ErrDBTransient) || !database.IsTransient(err) {
		return err
	}
	return fmt.Errorf("%w: %w", domain.ErrDBTransient, err)
}

func recordIfDBCallTimedOut(dbCtx context.Context, metrics ports.DBCallMetrics) {
	if errors.Is(context.Cause(dbCtx), errDBCallTimeout) {
		metrics.DBCallTimedOut()
	}
}
