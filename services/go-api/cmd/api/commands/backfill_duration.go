package commands

import (
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared/config"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

func RunBackfillDuration(cfg *config.Config, execute bool) {
	exitOnError(runBackfillDuration(cfg, execute))
}

func runBackfillDuration(cfg *config.Config, execute bool) error {
	ctx := context.Background()
	pool, err := openPool(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	audioStore, err := NewAudioStoreFromConfig(cfg)
	if err != nil {
		return err
	}

	tracks, err := loadReadyTracks(ctx, pool, " AND duration_seconds IS NULL", " ORDER BY added_at DESC")
	if err != nil {
		return err
	}

	fmt.Printf("\nFound %d tracks with missing duration...\n\n", len(tracks))

	if len(tracks) == 0 {
		fmt.Println("Nothing to do.")
		return nil
	}

	updated := 0
	skipped := 0
	errored := 0

	for i, t := range tracks {
		duration, err := probeDuration(ctx, audioStore, t.AudioRef)
		if err != nil {
			fmt.Printf("  [%d/%d] SKIP: %s — %s  (error: %v)\n", i+1, len(tracks), t.Title, t.Artist, err)
			skipped++
			continue
		}

		fmt.Printf("  [%d/%d] %s — %s  → %.1fs\n", i+1, len(tracks), t.Title, t.Artist, duration)

		if execute {
			_, err := pool.Exec(ctx,
				`UPDATE tracks SET duration_seconds = $3 WHERE id = $1 AND user_id = $2`,
				t.Id.UUID(), t.UserId.UUID(), duration)
			if err != nil {
				fmt.Printf("    ERROR updating: %v\n", err)
				errored++
				continue
			}
			updated++
		}
	}

	printSummary("Backfill duration complete:")
	fmt.Printf("  Total candidates:  %d\n", len(tracks))
	fmt.Printf("  Probed OK:         %d\n", len(tracks)-skipped)
	fmt.Printf("  Skipped:           %d\n", skipped)
	if execute {
		fmt.Printf("  Updated:           %d\n", updated)
		fmt.Printf("  Errors:            %d\n", errored)
	} else {
		printDryRunHint()
	}
	fmt.Println()

	slog.Info("backfill_duration_completed",
		"total", len(tracks),
		"skipped", skipped,
		"updated", updated,
		"errored", errored)
	return nil
}

func probeDuration(ctx context.Context, audioStore interface {
	Stream(ctx context.Context, audioRef string) (ports.AudioStream, int64, error)
}, audioRef string,
) (float64, error) {
	reader, _, err := audioStore.Stream(ctx, audioRef)
	if err != nil {
		return 0, fmt.Errorf("stream audio: %w", err)
	}
	defer func() { _ = reader.Close() }()

	tmpDir, err := os.MkdirTemp("", "backfill-dur-*")
	if err != nil {
		return 0, fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	tmpFile := filepath.Join(tmpDir, "audio.mp3")
	f, err := os.Create(tmpFile)
	if err != nil {
		return 0, fmt.Errorf("create temp file: %w", err)
	}
	if _, err := io.Copy(f, reader); err != nil {
		_ = f.Close()
		return 0, fmt.Errorf("copy audio to temp: %w", err)
	}
	if err := f.Close(); err != nil {
		return 0, fmt.Errorf("close temp file: %w", err)
	}

	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		tmpFile)
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe: %w", err)
	}

	var probe struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(output, &probe); err != nil {
		return 0, fmt.Errorf("parse ffprobe output: %w", err)
	}

	duration, err := strconv.ParseFloat(probe.Format.Duration, 64)
	if err != nil {
		return 0, fmt.Errorf("parse duration %q: %w", probe.Format.Duration, err)
	}

	if duration <= 0 {
		return 0, fmt.Errorf("invalid duration: %.2f", duration)
	}

	return duration, nil
}
