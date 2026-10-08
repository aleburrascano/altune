package commands

import (
	"altune/go-api/internal/catalog/adapters/persistence"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared/config"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const (
	sweepMinAge      = time.Hour
	stagingRefPrefix = "staging/"
)

func RunSweepStagingAudio(cfg *config.Config, execute bool) {
	exitOnError(runSweepStagingAudio(cfg, execute))
}

func runSweepStagingAudio(cfg *config.Config, execute bool) error {
	ctx := context.Background()

	pool, err := openPool(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	return sweepAudioStore(ctx, cfg, persistence.NewPgxTrackRepository(pool), execute)
}

func sweepAudioStore(ctx context.Context, cfg *config.Config, refs ports.AudioRefPrefixLister, execute bool) error {
	store, err := NewAudioStoreFromConfig(cfg)
	if err != nil {
		return err
	}
	lister, ok := store.(ports.AudioAgeLister)
	if !ok {
		return errors.New("the configured audio store cannot list object age")
	}

	deleted, keptReferenced, keptFresh, failed, err := sweepStaging(ctx, store, lister, refs, execute, time.Now())
	if err != nil {
		return err
	}
	return reportSweep(deleted, keptReferenced, keptFresh, failed, execute)
}

func reportSweep(deleted, keptReferenced, keptFresh, failed int, execute bool) error {
	printSummary("Sweep staging audio complete:")
	fmt.Printf("  Deleted:         %d\n", deleted)
	fmt.Printf("  Kept (in use):   %d\n", keptReferenced)
	fmt.Printf("  Kept (< 1h old): %d\n", keptFresh)
	fmt.Printf("  Failed:          %d\n", failed)
	if !execute {
		printDryRunHint()
	}
	fmt.Println()

	slog.Info("sweep_staging_audio_completed",
		"deleted", deleted, "kept_referenced", keptReferenced, "kept_fresh", keptFresh, "failed", failed)
	if failed > 0 {
		return fmt.Errorf("sweep-staging-audio: %d delete(s) failed", failed)
	}
	return nil
}

func oldEnoughToSweep(age time.Duration) bool {
	return age >= sweepMinAge
}

func sweepStaging(ctx context.Context, store ports.AudioStore, lister ports.AudioAgeLister, refs ports.AudioRefPrefixLister, execute bool, now time.Time) (deleted, keptReferenced, keptFresh, failed int, err error) {
	objects, err := lister.ListWithAge(ctx, stagingRefPrefix)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("list staging objects: %w", err)
	}

	referenced, err := refs.AudioRefsUnderPrefix(ctx, stagingRefPrefix)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("query referenced staging refs: %w", err)
	}

	fmt.Printf("\nFound %d object(s) under %q.\n\n", len(objects), stagingRefPrefix)

	deleted, keptReferenced, keptFresh, failed = sweepObjects(ctx, store, objects, referenced, execute, now)
	return deleted, keptReferenced, keptFresh, failed, nil
}

func sweepObjects(ctx context.Context, store ports.AudioStore, objects []ports.ObjectAge, referenced map[string]bool, execute bool, now time.Time) (deleted, keptReferenced, keptFresh, failed int) {
	for _, obj := range objects {
		age := now.Sub(obj.LastModified)
		if referenced[obj.AudioRef] {
			keptReferenced++
			continue
		}
		if !oldEnoughToSweep(age) {
			keptFresh++
			continue
		}

		if !sweepObject(ctx, store, obj, age, execute) {
			failed++
			continue
		}
		deleted++
	}
	return deleted, keptReferenced, keptFresh, failed
}

// sweepObject reports whether the unreferenced object was swept (or would be, on a dry run).
func sweepObject(ctx context.Context, store ports.AudioStore, obj ports.ObjectAge, age time.Duration, execute bool) bool {
	fmt.Printf("  %s (unreferenced, %s old)\n", obj.AudioRef, age.Round(time.Second))
	if !execute {
		return true
	}
	if err := store.Delete(ctx, obj.AudioRef); err != nil {
		fmt.Printf("    FAILED: %v\n", err)
		return false
	}
	return true
}
