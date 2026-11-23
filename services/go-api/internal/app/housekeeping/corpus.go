package housekeeping

import (
	"altune/go-api/internal/discovery"
	"context"
	"log/slog"
	"time"
)

func MaterializeBehavioralCorpus(store discovery.BehavioralLabelStore, path string) func(context.Context) error {
	builder := discovery.NewCorpusBuilder(store)
	return func(ctx context.Context) error {
		since := time.Now().UTC().Add(-30 * 24 * time.Hour)
		if err := builder.Materialize(ctx, since, since.Format("2006-01-02"), path); err != nil {
			slog.WarnContext(ctx, "behavioral corpus materialize failed", "error", err)
			return err
		}
		slog.InfoContext(ctx, "behavioral corpus materialized", "path", path)
		return nil
	}
}
