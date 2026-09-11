package commands

import (
	"context"
	"fmt"
	"log/slog"

	"altune/go-api/internal/shared/config"
)

const truncatedAudioThresholdSecs = 45.0

func RunReconcileTruncated(cfg *config.Config, execute bool) {
	ctx := context.Background()
	pool := mustOpenPool(ctx, cfg)
	defer pool.Close()

	audioStore := mustAudioStore(cfg)

	tracks := loadReadyTracks(ctx, pool, " AND duration_seconds IS NULL", " ORDER BY added_at DESC")

	fmt.Printf("\nFound %d ready tracks with missing duration...\n\n", len(tracks))
	if len(tracks) == 0 {
		fmt.Println("Nothing to do.")
		return
	}

	reacquired, backfilled, skipped, errored := 0, 0, 0, 0

	for i, t := range tracks {
		duration, err := probeDuration(ctx, audioStore, t.AudioRef)
		if err != nil {
			fmt.Printf("  [%d/%d] SKIP: %s — %s  (probe error: %v)\n", i+1, len(tracks), t.Title, t.Artist, err)
			skipped++
			continue
		}

		truncated := duration < truncatedAudioThresholdSecs
		action := "BACKFILL duration"
		if truncated {
			action = "RE-ACQUIRE (truncated)"
		}
		fmt.Printf("  [%d/%d] %.1fs  %s — %s  → %s\n", i+1, len(tracks), duration, t.Title, t.Artist, action)

		if !execute {
			continue
		}

		if truncated {
			err = markTrackFailed(ctx, pool, t.Id, t.UserId,
				"Only a short preview was downloaded — retry to re-acquire")
			if err == nil {
				reacquired++
			}
		} else {
			_, err = pool.Exec(ctx,
				`UPDATE tracks SET duration_seconds = $3 WHERE id = $1 AND user_id = $2`,
				t.Id.UUID(), t.UserId.UUID(), duration)
			if err == nil {
				backfilled++
			}
		}
		if err != nil {
			fmt.Printf("    ERROR updating: %v\n", err)
			errored++
		}
	}

	printSummary("Reconcile truncated complete:")
	fmt.Printf("  Total candidates:  %d\n", len(tracks))
	fmt.Printf("  Probe skipped:     %d\n", skipped)
	if execute {
		fmt.Printf("  Re-acquired:       %d  (marked failed → user taps retry)\n", reacquired)
		fmt.Printf("  Backfilled:        %d  (duration written in place)\n", backfilled)
		fmt.Printf("  Errors:            %d\n", errored)
	} else {
		printDryRunHint()
	}
	fmt.Println()

	slog.Info("reconcile_truncated_completed",
		"total", len(tracks),
		"skipped", skipped,
		"reacquired", reacquired,
		"backfilled", backfilled,
		"errored", errored)
}
