package persistence

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	sqlStateAdminShutdown        = "57P01"
	sqlStateCrashShutdown        = "57P02"
	sqlStateCannotConnectNow     = "57P03"
	sqlStateSerializationFailure = "40001"
	sqlStateDeadlockDetected     = "40P01"
	sqlStateClassConnection      = "08"
	sqlStateClassInsufficientRes = "53"
	undefinedTableCode           = "42P01"
	insufficientPrivCode         = "42501"
)

func isTransientFault(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return transientSQLState(pgErr.Code)
	}
	var connectErr *pgconn.ConnectError
	var netErr net.Error
	return errors.Is(err, context.DeadlineExceeded) ||
		errors.As(err, &connectErr) ||
		errors.As(err, &netErr) ||
		pgconn.SafeToRetry(err)
}

func transientSQLState(code string) bool {
	switch code {
	case sqlStateAdminShutdown, sqlStateCrashShutdown, sqlStateCannotConnectNow,
		sqlStateSerializationFailure, sqlStateDeadlockDetected:
		return true
	}
	return strings.HasPrefix(code, sqlStateClassConnection) || strings.HasPrefix(code, sqlStateClassInsufficientRes)
}

func isUnclassifiedFault(err error) bool {
	if err == nil {
		return false
	}
	return !errors.Is(err, context.DeadlineExceeded) &&
		!errors.Is(err, context.Canceled) &&
		!errors.Is(err, pgx.ErrNoRows)
}
