package service

import (
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"context"
	"fmt"
	"log/slog"
)

func PruneDiscoveryEvents(pruner ports.EventRetention, clk clock.Func) func(context.Context) error {
	return func(ctx context.Context) error {
		now := clk.UTC()
		discographyPruned, err := pruner.PruneDiscographyObserved(ctx, now)
		if err != nil {
			slog.WarnContext(ctx, "discography event prune failed", logging.KeyError, err)
			return fmt.Errorf("prune discography events: %w", err)
		}
		otherPruned, err := pruner.PruneEvents(ctx, now)
		if err != nil {
			slog.WarnContext(ctx, "discovery event retention prune failed", logging.KeyError, err)
			return fmt.Errorf("prune discovery events: %w", err)
		}
		if pruned := discographyPruned + otherPruned; pruned > 0 {
			slog.InfoContext(ctx, "discovery events pruned",
				"discography_rows", discographyPruned, "other_rows", otherPruned)
		}
		return nil
	}
}
