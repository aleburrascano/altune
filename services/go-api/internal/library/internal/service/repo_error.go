package service

import (
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"fmt"
	"log/slog"
)

func wrapRepoError(ctx context.Context, op string, err error) error {
	if errors.Is(err, domain.ErrDBTransient) {
		slog.WarnContext(ctx, "catalog.db_transient", "op", op, logging.KeyError, err)
		return fmt.Errorf("%s: %w", op, domain.ErrCatalogTemporarilyUnavailable)
	}
	return fmt.Errorf("%s: %w", op, err)
}
