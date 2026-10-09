package persistence

import (
	"altune/go-api/internal/shared/database"
	"context"
	"errors"
	"net"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	undefinedTableCode   = "42P01"
	insufficientPrivCode = "42501"
)

func isTransientFault(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return database.IsTransientSQLState(pgErr.Code)
	}
	var connectErr *pgconn.ConnectError
	var netErr net.Error
	return errors.Is(err, context.DeadlineExceeded) ||
		errors.As(err, &connectErr) ||
		errors.As(err, &netErr) ||
		pgconn.SafeToRetry(err)
}

func isUnclassifiedFault(err error) bool {
	if err == nil {
		return false
	}
	return !errors.Is(err, context.DeadlineExceeded) &&
		!errors.Is(err, context.Canceled) &&
		!errors.Is(err, pgx.ErrNoRows)
}
