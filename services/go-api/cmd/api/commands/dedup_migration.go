package commands

import (
	"altune/go-api/internal/shared/config"
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func RunDedupMigration(cfg *config.Config, execute bool) {
	exitOnError(runDedupMigration(cfg, execute))
}

func runDedupMigration(cfg *config.Config, execute bool) error {
	ctx := context.Background()
	pool, err := openPool(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	rows, err := pool.Query(ctx,
		`SELECT user_id, dedup_key,
			array_agg(id ORDER BY
				CASE acquisition_status
					WHEN 'ready' THEN 0 WHEN 'pending' THEN 1 ELSE 2
				END, added_at ASC
			) AS ids
		FROM tracks
		GROUP BY user_id, dedup_key
		HAVING count(*) > 1`)
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	type dupeGroup struct {
		userID   uuid.UUID
		dedupKey string
		ids      []uuid.UUID
	}
	var groups []dupeGroup
	for rows.Next() {
		var g dupeGroup
		if err := rows.Scan(&g.userID, &g.dedupKey, &g.ids); err != nil {
			return fmt.Errorf("scan failed: %w", err)
		}
		groups = append(groups, g)
	}

	if len(groups) == 0 {
		fmt.Println("\nNo duplicates found.")
		return nil
	}

	fmt.Printf("\nFound %d duplicate group(s):\n\n", len(groups))

	tracksDeleted := 0
	playlistsRemapped := 0

	for _, g := range groups {
		keepID := g.ids[0]
		deleteIDs := g.ids[1:]

		var title, artist, status string
		_ = pool.QueryRow(ctx,
			`SELECT title, artist, acquisition_status FROM tracks WHERE id = $1`,
			keepID).Scan(&title, &artist, &status)

		fmt.Printf("  Group: %s — %s\n", title, artist)
		fmt.Printf("    KEEP:   %s (status=%s)\n", keepID, status)
		for _, id := range deleteIDs {
			var delStatus string
			_ = pool.QueryRow(ctx,
				`SELECT acquisition_status FROM tracks WHERE id = $1`, id).Scan(&delStatus)
			fmt.Printf("    DELETE: %s (status=%s)\n", id, delStatus)
		}

		if execute {
			for _, deleteID := range deleteIDs {
				remapped := remapPlaylistTracks(ctx, pool, deleteID, keepID)
				playlistsRemapped += remapped
			}

			for _, deleteID := range deleteIDs {
				tag, _ := pool.Exec(ctx, `DELETE FROM tracks WHERE id = $1`, deleteID)
				tracksDeleted += int(tag.RowsAffected())
			}
		}
	}

	printSummary("Dedup migration complete:")
	fmt.Printf("  Duplicate groups: %d\n", len(groups))
	if execute {
		fmt.Printf("  Tracks deleted:   %d\n", tracksDeleted)
		fmt.Printf("  Playlists remapped: %d\n", playlistsRemapped)
	} else {
		printDryRunHint()
	}
	fmt.Println()

	slog.Info("dedup_migration_completed",
		"groups", len(groups),
		"deleted", tracksDeleted,
		"remapped", playlistsRemapped)
	return nil
}

func remapPlaylistTracks(ctx context.Context, pool *pgxpool.Pool, fromID, toID uuid.UUID) int {
	tag, _ := pool.Exec(ctx,
		`UPDATE playlist_tracks SET track_id = $1 WHERE track_id = $2`,
		toID, fromID)
	return int(tag.RowsAffected())
}
