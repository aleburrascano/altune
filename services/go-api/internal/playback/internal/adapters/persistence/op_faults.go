package persistence

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

func isUnclassifiedFault(err error) bool {
	if err == nil {
		return false
	}
	return !errors.Is(err, context.DeadlineExceeded) &&
		!errors.Is(err, context.Canceled) &&
		!errors.Is(err, pgx.ErrNoRows)
}
