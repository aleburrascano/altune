package commands

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"

	"github.com/google/uuid"
)

func RunFixAudioRefs(cfg *config.Config, execute bool) {
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

	rows, err := pool.Query(ctx,
		`SELECT id, audio_ref FROM tracks WHERE audio_ref IS NOT NULL`)
	if err != nil {
		fmt.Printf("ERROR: query failed: %v\n", err)
		os.Exit(1)
	}
	defer rows.Close()

	type refRow struct {
		id       uuid.UUID
		audioRef string
	}
	var needsFix []refRow
	total := 0

	for rows.Next() {
		var r refRow
		if err := rows.Scan(&r.id, &r.audioRef); err != nil {
			continue
		}
		total++
		if hasUUIDPrefix(r.audioRef) {
			needsFix = append(needsFix, r)
		}
	}

	fmt.Printf("\nScanned %d tracks with audio_ref.\n", total)
	fmt.Printf("Found %d with UUID prefix to strip.\n\n", len(needsFix))

	if len(needsFix) == 0 {
		fmt.Println("Nothing to fix.\n")
		return
	}

	fixed := 0
	for _, r := range needsFix {
		newRef := stripUUIDPrefix(r.audioRef)
		fmt.Printf("  %s → %s\n", r.audioRef, newRef)

		if execute {
			_, err := pool.Exec(ctx,
				`UPDATE tracks SET audio_ref = $1 WHERE id = $2`,
				newRef, r.id)
			if err == nil {
				fixed++
			}
		}
	}

	fmt.Printf("\n%s\n", "==================================================")
	if execute {
		fmt.Printf("Fixed %d audio refs.\n", fixed)
	} else {
		fmt.Println("Run with --execute to apply changes.")
	}
	fmt.Println()

	slog.Info("fix_audio_refs_completed", "total", total, "needs_fix", len(needsFix), "fixed", fixed)
}

func hasUUIDPrefix(ref string) bool {
	parts := strings.SplitN(ref, "/", 2)
	if len(parts) < 2 {
		return false
	}
	_, err := uuid.Parse(parts[0])
	return err == nil && len(parts[0]) == 36
}

func stripUUIDPrefix(ref string) string {
	parts := strings.SplitN(ref, "/", 2)
	if len(parts) < 2 {
		return ref
	}
	return parts[1]
}
