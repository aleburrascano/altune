package commands

import (
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared/config"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const sweepMinAge = time.Hour

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

	store, err := NewAudioStoreFromConfig(cfg)
	if err != nil {
		return err
	}
	lister, ok := store.(ports.AudioAgeLister)
	if !ok {
		return errors.New("the configured audio store cannot list object age")
	}

	objects, err := lister.ListWithAge(ctx, stagingRefPrefix)
	if err != nil {
		return fmt.Errorf("list staging objects: %w", err)
	}

	referenced, err := referencedStagingRefs(ctx, pool)
	if err != nil {
		return err
	}

	now := time.Now()
	fmt.Printf("\nFound %d object(s) under %q.\n\n", len(objects), stagingRefPrefix)

	var deleted, keptReferenced, keptFresh, failed int
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

		fmt.Printf("  %s (unreferenced, %s old)\n", obj.AudioRef, age.Round(time.Second))
		if !execute {
			deleted++
			continue
		}
		if err := store.Delete(ctx, obj.AudioRef); err != nil {
			fmt.Printf("    FAILED: %v\n", err)
			failed++
			continue
		}
		deleted++
	}

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

func referencedStagingRefs(ctx context.Context, pool *pgxpool.Pool) (map[string]bool, error) {
	rows, err := pool.Query(ctx,
		`SELECT audio_ref FROM tracks WHERE audio_ref LIKE 'staging/%'`)
	if err != nil {
		return nil, fmt.Errorf("query referenced staging refs: %w", err)
	}
	defer rows.Close()

	referenced := map[string]bool{}
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return nil, fmt.Errorf("scan referenced staging ref: %w", err)
		}
		referenced[ref] = true
	}
	return referenced, rows.Err()
}
