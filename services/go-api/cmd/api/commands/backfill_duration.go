package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"
)

func RunBackfillDuration(cfg *config.Config, execute bool) {
	if cfg.DatabaseURL == "" {
		fmt.Println("ERROR: DATABASE_URL not set")
		os.Exit(1)
	}

	ctx := context.Background()
	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Printf("ERROR: database connection failed: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	audioStore := buildAudioStoreForCLI(cfg)
	if audioStore == nil {
		fmt.Println("ERROR: no audio store configured (need MUSIC_DIR or OCI_S3_* env vars)")
		os.Exit(1)
	}

	rows, err := pool.Query(ctx,
		`SELECT id, user_id, title, artist, audio_ref
		FROM tracks
		WHERE duration_seconds IS NULL
		  AND audio_ref IS NOT NULL
		  AND acquisition_status = 'ready'
		ORDER BY added_at DESC`)
	if err != nil {
		fmt.Printf("ERROR: query failed: %v\n", err)
		os.Exit(1)
	}
	defer rows.Close()

	type trackRow struct {
		id, userId, title, artist, audioRef string
	}
	var tracks []trackRow
	for rows.Next() {
		var t trackRow
		if err := rows.Scan(&t.id, &t.userId, &t.title, &t.artist, &t.audioRef); err != nil {
			fmt.Printf("ERROR: scan failed: %v\n", err)
			os.Exit(1)
		}
		tracks = append(tracks, t)
	}

	fmt.Printf("\nFound %d tracks with missing duration...\n\n", len(tracks))

	if len(tracks) == 0 {
		fmt.Println("Nothing to do.")
		return
	}

	updated := 0
	skipped := 0
	errored := 0

	for i, t := range tracks {
		duration, err := probeDuration(ctx, audioStore, t.audioRef)
		if err != nil {
			fmt.Printf("  [%d/%d] SKIP: %s — %s  (error: %v)\n", i+1, len(tracks), t.title, t.artist, err)
			skipped++
			continue
		}

		fmt.Printf("  [%d/%d] %s — %s  → %.1fs\n", i+1, len(tracks), t.title, t.artist, duration)

		if execute {
			_, err := pool.Exec(ctx,
				`UPDATE tracks SET duration_seconds = $3 WHERE id = $1 AND user_id = $2`,
				t.id, t.userId, duration)
			if err != nil {
				fmt.Printf("    ERROR updating: %v\n", err)
				errored++
				continue
			}
			updated++
		}
	}

	fmt.Printf("\n%s\n", "==================================================")
	fmt.Println("Backfill duration complete:")
	fmt.Printf("  Total candidates:  %d\n", len(tracks))
	fmt.Printf("  Probed OK:         %d\n", len(tracks)-skipped)
	fmt.Printf("  Skipped:           %d\n", skipped)
	if execute {
		fmt.Printf("  Updated:           %d\n", updated)
		fmt.Printf("  Errors:            %d\n", errored)
	} else {
		fmt.Println("\n  Run with --execute to apply changes.")
	}
	fmt.Println()

	slog.Info("backfill_duration_completed",
		"total", len(tracks),
		"skipped", skipped,
		"updated", updated,
		"errored", errored)
}

func probeDuration(ctx context.Context, audioStore interface {
	Stream(ctx context.Context, audioRef string) (io.ReadCloser, int64, error)
}, audioRef string) (float64, error) {
	reader, _, err := audioStore.Stream(ctx, audioRef)
	if err != nil {
		return 0, fmt.Errorf("stream audio: %w", err)
	}
	defer reader.Close()

	tmpDir, err := os.MkdirTemp("", "backfill-dur-*")
	if err != nil {
		return 0, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tmpFile := filepath.Join(tmpDir, "audio.mp3")
	f, err := os.Create(tmpFile)
	if err != nil {
		return 0, fmt.Errorf("create temp file: %w", err)
	}
	if _, err := io.Copy(f, reader); err != nil {
		f.Close()
		return 0, fmt.Errorf("copy audio to temp: %w", err)
	}
	f.Close()

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
