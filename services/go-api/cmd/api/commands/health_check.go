package commands

import (
	"altune/go-api/internal/catalog/adapters/persistence"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared/config"
	"context"
	"errors"
	"fmt"
	"log/slog"
)

func RunHealthCheck(cfg *config.Config, fix bool) {
	exitOnError(runHealthCheck(cfg, fix))
}

func runHealthCheck(cfg *config.Config, fix bool) error {
	ctx := context.Background()
	pool, err := openPool(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := persistence.NewPgxTrackRepository(pool)
	tracks, err := repo.ListReadyAudioTracks(ctx)
	if err != nil {
		return err
	}
	totalChecked := len(tracks)
	fmt.Printf("\nChecking %d tracks with status=ready...\n\n", totalChecked)

	audioStore, err := NewAudioStoreFromConfig(cfg)
	if err != nil {
		return err
	}

	orphanedDB, fixed := checkReadyTracks(ctx, tracks, audioStore, repo, fix)

	printSummary("Health check complete:")
	fmt.Printf("  Tracks checked:    %d\n", totalChecked)
	fmt.Printf("  Orphaned DB rows:  %d\n", orphanedDB)
	if fix {
		fmt.Printf("  Fixed:             %d\n", fixed)
	} else if orphanedDB > 0 {
		fmt.Println("\n  Run with --fix to mark orphaned tracks as failed.")
	}
	fmt.Println()

	slog.Info("health_check_completed",
		"total_checked", totalChecked,
		"orphaned_db", orphanedDB,
		"fixed", fixed)
	return nil
}

const missingAudioReason = "Audio file missing from storage (health-check)"

func checkReadyTracks(ctx context.Context, tracks []ports.ReadyAudioTrack, store ports.AudioStore, repo ports.TrackReadWriter, fix bool) (orphaned, fixed int) {
	for _, t := range tracks {
		exists, err := store.Exists(ctx, t.AudioRef)
		if err == nil && exists {
			continue
		}
		orphaned++
		fmt.Printf("  ORPHANED: %s — %s  (id=%s, ref=%s)\n", t.Title, t.Artist, t.Id, t.AudioRef)

		if fix {
			_ = markTrackFailed(ctx, repo, t)
			fixed++
			fmt.Println("    → FIXED: marked as failed")
		}
	}
	return orphaned, fixed
}

func markTrackFailed(ctx context.Context, repo ports.TrackReadWriter, t ports.ReadyAudioTrack) error {
	track, err := repo.GetByID(ctx, t.Id, t.UserId)
	if err != nil {
		return err
	}
	if track == nil {
		return errors.New("track disappeared")
	}
	if err := track.MarkFailed(missingAudioReason); err != nil {
		return err
	}
	return repo.Update(ctx, track, track.Version)
}
