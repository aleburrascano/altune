package domain

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

var ErrDBTransient = errors.New("catalog database temporarily unavailable")

func WrapRepoError(ctx context.Context, op string, err error) error {
	if errors.Is(err, ErrDBTransient) {
		slog.WarnContext(ctx, "catalog.db_transient", "op", op, "error", err)
		return fmt.Errorf("%s: %w: %w", op, ErrCatalogTemporarilyUnavailable, err)
	}
	return fmt.Errorf("%s: %w", op, err)
}
