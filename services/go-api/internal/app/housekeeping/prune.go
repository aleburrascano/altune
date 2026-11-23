package housekeeping

import (
	"context"
	"log/slog"
	"time"
)

type DiscoveryEventPruner interface {
	PruneDiscographyObserved(ctx context.Context, now time.Time) (int64, error)
	PruneEvents(ctx context.Context, now time.Time) (int64, error)
}

func PruneDiscoveryEvents(pruner DiscoveryEventPruner) func(context.Context) error {
	return func(ctx context.Context) error {
		now := time.Now().UTC()
		discographyPruned, err := pruner.PruneDiscographyObserved(ctx, now)
		if err != nil {
			slog.WarnContext(ctx, "discography event prune failed", "error", err)
			return err
		}
		otherPruned, err := pruner.PruneEvents(ctx, now)
		if err != nil {
			slog.WarnContext(ctx, "discovery event retention prune failed", "error", err)
			return err
		}
		if pruned := discographyPruned + otherPruned; pruned > 0 {
			slog.InfoContext(ctx, "discovery events pruned",
				"discography_rows", discographyPruned, "other_rows", otherPruned)
		}
		return nil
	}
}

type RetentionPruner interface {
	Prune(ctx context.Context, now time.Time) (int64, error)
}

func PruneAcquisitionRows(outcomes, rejections RetentionPruner) func(context.Context) error {
	return func(ctx context.Context) error {
		now := time.Now().UTC()
		outcomesPruned, err := outcomes.Prune(ctx, now)
		if err != nil {
			slog.WarnContext(ctx, "acquisition outcome prune failed", "error", err)
			return err
		}
		rejectionsPruned, err := rejections.Prune(ctx, now)
		if err != nil {
			slog.WarnContext(ctx, "acquisition rejection prune failed", "error", err)
			return err
		}
		if outcomesPruned+rejectionsPruned > 0 {
			slog.InfoContext(ctx, "acquisition rows pruned",
				"outcome_rows", outcomesPruned, "rejection_rows", rejectionsPruned)
		}
		return nil
	}
}
