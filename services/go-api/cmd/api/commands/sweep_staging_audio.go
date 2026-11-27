package commands

import (
	"altune/go-api/cmd/api/commands/stagingsweep"
	"altune/go-api/internal/library"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/config"
	"context"
	"errors"
	"fmt"
	"log/slog"
)

func RunSweepStagingAudio(cfg *config.Config, mode RunMode) error {
	return runSweepStagingAudio(cfg, mode)
}

func runSweepStagingAudio(cfg *config.Config, mode RunMode) error {
	ctx := context.Background()

	pool, err := openPool(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	return sweepAudioStore(ctx, cfg, library.NewPostgresTrackRepository(pool, library.NoopDBCallMetrics()), mode)
}

func sweepAudioStore(ctx context.Context, cfg *config.Config, refs library.AudioRefPrefixLister, mode RunMode) error {
	store, err := library.NewAudioStoreFromConfig(cfg, slog.Default(), library.NoopFSCallMetrics())
	if err != nil {
		return err
	}
	lister, ok := store.(library.AudioAgeLister)
	if !ok {
		return errors.New("the configured audio store cannot list object age")
	}

	var tally stagingsweep.Tally
	if mode == Apply {
		tally, err = stagingsweep.Sweep(ctx, store, lister, refs, clock.Now())
	} else {
		tally, err = stagingsweep.Preview(ctx, lister, refs, clock.Now())
	}
	if err != nil {
		return err
	}
	return reportSweep(ctx, tally.Deleted, tally.KeptReferenced, tally.KeptFresh, tally.Failed, mode)
}

func reportSweep(ctx context.Context, deleted, keptReferenced, keptFresh, failed int, mode RunMode) error {
	printSummary("Sweep staging audio complete:")
	fmt.Printf("  Deleted:         %d\n", deleted)
	fmt.Printf("  Kept (in use):   %d\n", keptReferenced)
	fmt.Printf("  Kept (< 1h old): %d\n", keptFresh)
	fmt.Printf("  Failed:          %d\n", failed)
	if mode != Apply {
		printDryRunHint()
	}
	fmt.Println()

	slog.InfoContext(ctx, "sweep_staging_audio_completed",
		"deleted", deleted, "kept_referenced", keptReferenced, "kept_fresh", keptFresh, "failed", failed)
	if failed > 0 {
		return fmt.Errorf("sweep-staging-audio: %d delete(s) failed", failed)
	}
	return nil
}
