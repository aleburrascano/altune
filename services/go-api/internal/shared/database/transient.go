package database

import (
	"context"
	"errors"
	"io"
	"net"

	"github.com/jackc/pgx/v5/pgconn"
)

func IsTransient(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return IsTransientSQLState(pgErr.Code)
	}
	var connectErr *pgconn.ConnectError
	var netErr net.Error
	return errors.As(err, &connectErr) || errors.As(err, &netErr) || errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, pgconn.ErrConnClosed)
}
