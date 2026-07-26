package commands

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"altune/go-api/internal/acquisition/adapters/id3"
	"altune/go-api/internal/acquisition/adapters/ytdlp"
	acqService "altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"
)

const perTrackTimeout = 10 * time.Minute

type reacquireSpec struct {
	audioRefLikePattern     string
	bannerFormatWithCount   string
	doneHeading             string
	successLabel            string
	orphanLogMsg            string
	completedLogEventOrNone string
}

func runReacquire(cfg *config.Config, execute bool, limit int, spec reacquireSpec) {
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
	searcher := ytdlp.NewYtDlpAudioSearcher(cfg.FFmpegLocation, cfg.YtDLPCookieFile, cfg.YtDLPJSRuntime)
	prober := ytdlp.NewFfprobeProber(cfg.FFmpegLocation)

	rows, err := pool.Query(ctx,
		`SELECT id, user_id, title, artist, album, audio_ref, duration_seconds
		FROM tracks
		WHERE acquisition_status = 'ready'
		  AND audio_ref IS NOT NULL
		  AND audio_ref LIKE '`+spec.audioRefLikePattern+`'
		ORDER BY added_at DESC`)
	if err != nil {
		fmt.Printf("ERROR: query failed: %v\n", err)
		os.Exit(1)
	}
	defer rows.Close()

	type trackRow struct {
		id, userId, title, artist, oldRef string
		album                             *string
		duration                          *float64
	}
	var tracks []trackRow
	for rows.Next() {
		var t trackRow
		if err := rows.Scan(&t.id, &t.userId, &t.title, &t.artist, &t.album, &t.oldRef, &t.duration); err != nil {
			fmt.Printf("ERROR: scan failed: %v\n", err)
			os.Exit(1)
		}
		tracks = append(tracks, t)
	}
	if limit > 0 && len(tracks) > limit {
		tracks = tracks[:limit]
	}

	fmt.Printf("\n"+spec.bannerFormatWithCount+"\n\n", len(tracks))
	if len(tracks) == 0 {
		fmt.Println("Nothing to do.")
		return
	}

	if !execute {
		for i, t := range tracks {
			fmt.Printf("  [%d/%d] %s — %s\n      %s\n", i+1, len(tracks), t.title, t.artist, t.oldRef)
		}
		fmt.Println("\n  Run with --execute to apply (add --limit N to convert only the first N).")
		return
	}

	sources := acqService.NewSourceRegistry(ytdlp.NewSource(searcher))
	steps := acqService.CoreSteps(sources, id3.NewTagger(), audioStore, prober, nil)

	fixed, skipped := 0, 0
	for i, t := range tracks {
		if err := ctx.Err(); err != nil {
			fmt.Printf("  Cancelled: %v\n", err)
			break
		}

		album := ""
		if t.album != nil {
			album = *t.album
		}
		track := acqService.TrackRef{ID: t.id, UserID: t.userId, Title: t.title, Artist: t.artist, Album: album}
		newRef, err := reacquireTrack(ctx, steps, audioStore, track, t.oldRef, t.duration)
		if err != nil {
			fmt.Printf("  [%d/%d] SKIP: %s — %s  (%v)\n", i+1, len(tracks), t.title, t.artist, err)
			skipped++
			continue
		}

		if _, err := pool.Exec(ctx,
			`UPDATE tracks SET audio_ref = $1 WHERE id = $2 AND user_id = $3`,
			newRef, t.id, t.userId); err != nil {
			fmt.Printf("  [%d/%d] SKIP (db update failed, old ref and old file left intact): %s — %s  (%v)\n", i+1, len(tracks), t.title, t.artist, err)
			skipped++
			continue
		}

		if newRef != t.oldRef {
			if err := audioStore.Delete(ctx, t.oldRef); err != nil {
				slog.WarnContext(ctx, spec.orphanLogMsg,
					"track_id", t.id, "old_ref", t.oldRef, "error", err)
			}
		}
		fmt.Printf("  [%d/%d] OK: %s — %s  -> %s\n", i+1, len(tracks), t.title, t.artist, newRef)
		fixed++
	}

	fmt.Printf("\n%s\n", strings.Repeat("=", 50))
	fmt.Println(spec.doneHeading)
	fmt.Printf("  Candidates: %d\n", len(tracks))
	fmt.Printf("  %-11s %d\n", spec.successLabel+":", fixed)
	fmt.Printf("  Skipped:    %d  (old file kept, safe to re-run)\n", skipped)
	fmt.Println()

	if spec.completedLogEventOrNone != "" {
		slog.Info(spec.completedLogEventOrNone,
			"candidates", len(tracks),
			"converted", fixed,
			"skipped", skipped)
	}
}

func reacquireTrack(
	ctx context.Context,
	steps []acqService.Step,
	audioStore ports.AudioStore,
	track acqService.TrackRef,
	oldRef string,
	dbDuration *float64,
) (string, error) {
	perCtx, cancel := context.WithTimeout(ctx, perTrackTimeout)
	defer cancel()

	track.Duration = expectedDuration(perCtx, audioStore, dbDuration, oldRef)

	ac := &acqService.AcquisitionContext{Track: track}

	runErr := acqService.RunPipeline(perCtx, steps, ac)
	acqService.CleanupTemp(perCtx, ac)
	if runErr != nil {
		return "", runErr
	}
	if ac.AudioRef == "" {
		return "", fmt.Errorf("pipeline produced no audio ref")
	}
	return ac.AudioRef, nil
}

func expectedDuration(
	ctx context.Context,
	audioStore ports.AudioStore,
	dbDuration *float64,
	oldRef string,
) float64 {
	if dbDuration != nil && *dbDuration > 0 {
		return *dbDuration
	}
	probed, err := probeDuration(ctx, audioStore, oldRef)
	if err != nil {
		return 0
	}
	return probed
}
