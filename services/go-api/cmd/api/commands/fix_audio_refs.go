package commands

import (
	"altune/go-api/internal/shared/config"
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
)

func RunFixAudioRefs(cfg *config.Config, execute bool) {
	exitOnError(runFixAudioRefs(cfg, execute))
}

func runFixAudioRefs(cfg *config.Config, execute bool) error {
	ctx := context.Background()
	pool, err := openPool(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	rows, err := pool.Query(ctx,
		`SELECT id, audio_ref FROM tracks WHERE audio_ref IS NOT NULL`)
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
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
		fmt.Println("Nothing to fix.")
		return nil
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
	return nil
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
