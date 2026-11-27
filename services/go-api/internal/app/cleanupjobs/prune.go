package cleanupjobs

import (
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"context"
	"fmt"
	"log/slog"
	"time"
)

type DiscoveryEventPruner interface {
	PruneDiscographyObserved(ctx context.Context, now time.Time) (int64, error)
	PruneEvents(ctx context.Context, now time.Time) (int64, error)
}

func PruneDiscoveryEvents(pruner DiscoveryEventPruner, clk clock.Func) func(context.Context) error {
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

func PruneClientEvents(pruner RetentionPruner, clk clock.Func) func(context.Context) error {
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

type RetentionPruner interface {
	Prune(ctx context.Context, now time.Time) (int64, error)
}

func PruneAcquisitionRows(outcomes, rejections RetentionPruner, clk clock.Func) func(context.Context) error {
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
