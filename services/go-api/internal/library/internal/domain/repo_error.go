package domain

import (
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"fmt"
	"log/slog"
)

var ErrDBTransient = errors.New("catalog database temporarily unavailable")

func WrapRepoError(ctx context.Context, op string, err error) error {
	if errors.Is(err, ErrDBTransient) {
		slog.WarnContext(ctx, "catalog.db_transient", "op", op, logging.KeyError, err)
		return fmt.Errorf("%s: %w", op, ErrCatalogTemporarilyUnavailable)
	}
	return fmt.Errorf("%s: %w", op, err)
}
