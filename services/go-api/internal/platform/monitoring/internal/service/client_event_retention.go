package service

import (
	"altune/go-api/internal/platform/monitoring/internal/ports"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"context"
	"fmt"
	"log/slog"
)

func PruneClientEvents(pruner ports.ClientEventPruner, clk clock.Func) func(context.Context) error {
	return func(ctx context.Context) error {
		pruned, err := pruner.Prune(ctx, clk.UTC())
		if err != nil {
			slog.WarnContext(ctx, "client event retention prune failed", logging.KeyError, err)
			return fmt.Errorf("prune client events: %w", err)
		}
		if pruned > 0 {
			slog.InfoContext(ctx, "client events pruned", "rows", pruned)
		}
		return nil
	}
}

func EraseClientEventsOfDeletedIdentities(ctx context.Context, eraser ports.ClientEventEraser) error {
	erased, err := eraser.EraseRowsOfDeletedIdentities(ctx)
	if err != nil {
		return fmt.Errorf("erase client events of deleted identities: %w", err)
	}
	if erased > 0 {
		slog.InfoContext(ctx, "client_events.deleted_identity_rows_erased", "rows", erased)
	}
	return nil
}
