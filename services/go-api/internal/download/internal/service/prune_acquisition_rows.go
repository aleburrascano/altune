package service

import (
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"context"
	"fmt"
	"log/slog"
)

func PruneAcquisitionRows(outcomes, rejections ports.RetentionPruner, clk clock.Func) func(context.Context) error {
	return func(ctx context.Context) error {
		now := clk.UTC()
		outcomesPruned, err := outcomes.Prune(ctx, now)
		if err != nil {
			slog.WarnContext(ctx, "acquisition outcome prune failed", logging.KeyError, err)
			return fmt.Errorf("prune acquisition outcomes: %w", err)
		}
		rejectionsPruned, err := rejections.Prune(ctx, now)
		if err != nil {
			slog.WarnContext(ctx, "acquisition rejection prune failed", logging.KeyError, err)
			return fmt.Errorf("prune acquisition rejections: %w", err)
		}
		if outcomesPruned+rejectionsPruned > 0 {
			slog.InfoContext(ctx, "acquisition rows pruned",
				"outcome_rows", outcomesPruned, "rejection_rows", rejectionsPruned)
		}
		return nil
	}
}
