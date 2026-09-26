package commands

import (
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const stagingRefPrefix = "staging/"

func RunPromoteStaging(cfg *config.Config, execute bool) {
	exitOnError(runPromoteStaging(cfg, execute))
}

type accountMap map[uuid.UUID]uuid.UUID

func runPromoteStaging(cfg *config.Config, execute bool) error {
	ctx := context.Background()

	prodPool, err := openPool(ctx, cfg)
	if err != nil {
		return err
	}
	defer prodPool.Close()

	stagingURL := os.Getenv("STAGING_DATABASE_URL")
	if stagingURL == "" {
		return errors.New("STAGING_DATABASE_URL not set")
	}
	stagingPool, err := database.NewPool(ctx, stagingURL, cfg.DBPoolMaxConns)
	if err != nil {
		return fmt.Errorf("staging database connection failed: %w", err)
	}
	defer stagingPool.Close()

	store, err := NewAudioStoreFromConfig(cfg)
	if err != nil {
		return err
	}
	copier, ok := store.(ports.AudioCopier)
	if !ok {
		return errors.New("the configured audio store cannot copy objects")
	}

	accounts, err := loadAccountMap(ctx, prodPool, stagingPool)
	if err != nil {
		return err
	}
	if len(accounts) == 0 {
		fmt.Println("\nNo prod account has a staging twin; nothing to promote.")
		return nil
	}

	sharedCols, err := sharedTrackColumns(ctx, prodPool, stagingPool)
	if err != nil {
		return err
	}

	candidates, err := loadPromotionCandidates(ctx, stagingPool, accounts)
	if err != nil {
		return err
	}

	fmt.Printf("\nFound %d ready staging track(s) to consider for promotion.\n\n", len(candidates))

	var promoted, skipped, failed int
	for _, c := range candidates {
		prodUserID := prodUserIDFor(accounts, c.stagingUserID)
		if prodUserID == uuid.Nil {
			fmt.Printf("  FAILED %s: staging user %s is not in the account map\n", c.trackID, c.stagingUserID)
			failed++
			continue
		}

		prodRef, err := prodRefFor(c.audioRef, c.stagingUserID, prodUserID)
		if err != nil {
			fmt.Printf("  FAILED %s: %v\n", c.trackID, err)
			failed++
			continue
		}

		reason, skip, err := decidePromotionSkip(ctx, prodPool, store, prodUserID, c.dedupKey, prodRef)
		if err != nil {
			fmt.Printf("  FAILED %s: %v\n", c.trackID, err)
			failed++
			continue
		}
		if skip {
			fmt.Printf("  SKIP %s: %s\n", c.trackID, reason)
			skipped++
			continue
		}

		fmt.Printf("  %s: %s -> %s\n", c.trackID, c.audioRef, prodRef)
		if !execute {
			promoted++
			continue
		}

		if err := promoteOne(ctx, prodPool, stagingPool, store, copier, sharedCols, c, prodUserID, prodRef); err != nil {
			fmt.Printf("  FAILED %s: %v\n", c.trackID, err)
			failed++
			continue
		}
		promoted++
	}

	printSummary("Promote staging complete:")
	fmt.Printf("  Promoted: %d\n", promoted)
	fmt.Printf("  Skipped:  %d\n", skipped)
	fmt.Printf("  Failed:   %d\n", failed)
	if !execute {
		printDryRunHint()
	}
	fmt.Println()

	slog.Info("promote_staging_completed", "promoted", promoted, "skipped", skipped, "failed", failed)
	if failed > 0 {
		return fmt.Errorf("promote-staging: %d promotion(s) failed", failed)
	}
	return nil
}

func prodUserIDFor(accounts accountMap, stagingID uuid.UUID) uuid.UUID {
	for prodID, sid := range accounts {
		if sid == stagingID {
			return prodID
		}
	}
	return uuid.Nil
}

func prodRefFor(stagingRef string, stagingUserID, prodUserID uuid.UUID) (string, error) {
	rest, ok := strings.CutPrefix(stagingRef, stagingRefPrefix)
	if !ok {
		return "", fmt.Errorf("not a staging ref: %q", stagingRef)
	}
	firstSegment, remainder, ok := strings.Cut(rest, "/")
	if !ok || firstSegment == "" || remainder == "" {
		return "", fmt.Errorf("staging ref has no user segment: %q", stagingRef)
	}
	if firstSegment != stagingUserID.String() {
		return "", fmt.Errorf("staging ref user segment %q does not match owning account %s", firstSegment, stagingUserID)
	}
	return prodUserID.String() + "/" + remainder, nil
}

func decidePromotionSkip(ctx context.Context, prodPool *pgxpool.Pool, store ports.AudioStore, prodUserID uuid.UUID, dedupKey, prodRef string) (string, bool, error) {
	var dedupExists bool
	err := prodPool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM tracks WHERE user_id = $1 AND dedup_key = $2)`,
		prodUserID, dedupKey).Scan(&dedupExists)
	if err != nil {
		return "", false, fmt.Errorf("check prod dedup: %w", err)
	}
	if dedupExists {
		return "prod already has a track with this (user_id, dedup_key)", true, nil
	}

	objectExists, err := store.Exists(ctx, prodRef)
	if err != nil {
		return "", false, fmt.Errorf("check prod object %q: %w", prodRef, err)
	}
	if objectExists {
		return "an object already exists at the prod ref", true, nil
	}
	return "", false, nil
}

type promotionCandidate struct {
	trackID       uuid.UUID
	stagingUserID uuid.UUID
	dedupKey      string
	audioRef      string
}

func loadAccountMap(ctx context.Context, prodPool, stagingPool *pgxpool.Pool) (accountMap, error) {
	stagingRows, err := stagingPool.Query(ctx, `SELECT id, lower(email) FROM auth.users WHERE email IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("query staging accounts: %w", err)
	}
	stagingByEmail := map[string]uuid.UUID{}
	for stagingRows.Next() {
		var id uuid.UUID
		var email string
		if err := stagingRows.Scan(&id, &email); err != nil {
			stagingRows.Close()
			return nil, fmt.Errorf("scan staging account: %w", err)
		}
		stagingByEmail[email] = id
	}
	stagingRows.Close()
	if err := stagingRows.Err(); err != nil {
		return nil, fmt.Errorf("read staging accounts: %w", err)
	}

	prodRows, err := prodPool.Query(ctx, `SELECT id, lower(email) FROM auth.users WHERE email IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("query prod accounts: %w", err)
	}
	defer prodRows.Close()

	accounts := accountMap{}
	for prodRows.Next() {
		var id uuid.UUID
		var email string
		if err := prodRows.Scan(&id, &email); err != nil {
			return nil, fmt.Errorf("scan prod account: %w", err)
		}
		if stagingID, ok := stagingByEmail[email]; ok {
			accounts[id] = stagingID
		}
	}
	if err := prodRows.Err(); err != nil {
		return nil, fmt.Errorf("read prod accounts: %w", err)
	}
	return accounts, nil
}

func loadPromotionCandidates(ctx context.Context, stagingPool *pgxpool.Pool, accounts accountMap) ([]promotionCandidate, error) {
	stagingIDs := make([]uuid.UUID, 0, len(accounts))
	for _, stagingID := range accounts {
		stagingIDs = append(stagingIDs, stagingID)
	}

	tx, err := stagingPool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("begin staging read-only tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx,
		`SELECT id, user_id, dedup_key, audio_ref FROM tracks
			WHERE user_id = ANY($1) AND acquisition_status = 'ready' AND audio_ref LIKE 'staging/%'
			ORDER BY added_at`,
		stagingIDs)
	if err != nil {
		return nil, fmt.Errorf("query staging candidates: %w", err)
	}
	defer rows.Close()

	var candidates []promotionCandidate
	for rows.Next() {
		var c promotionCandidate
		if err := rows.Scan(&c.trackID, &c.stagingUserID, &c.dedupKey, &c.audioRef); err != nil {
			return nil, fmt.Errorf("scan staging candidate: %w", err)
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read staging candidates: %w", err)
	}
	return candidates, tx.Commit(ctx)
}

func sharedTrackColumns(ctx context.Context, prodPool, stagingPool *pgxpool.Pool) ([]string, error) {
	prodCols, err := trackColumnNames(ctx, prodPool)
	if err != nil {
		return nil, fmt.Errorf("prod tracks columns: %w", err)
	}
	stagingCols, err := trackColumnNames(ctx, stagingPool)
	if err != nil {
		return nil, fmt.Errorf("staging tracks columns: %w", err)
	}
	prodSet := map[string]bool{}
	for _, c := range prodCols {
		prodSet[c] = true
	}
	var shared []string
	for _, c := range stagingCols {
		if prodSet[c] {
			shared = append(shared, c)
		}
	}
	if len(shared) == 0 {
		return nil, errors.New("no shared tracks columns between prod and staging")
	}
	return shared, nil
}

func trackColumnNames(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx,
		`SELECT column_name FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'tracks' AND is_generated = 'NEVER'
			ORDER BY ordinal_position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			return nil, err
		}
		cols = append(cols, col)
	}
	return cols, rows.Err()
}

func promoteOne(ctx context.Context, prodPool, stagingPool *pgxpool.Pool, store ports.AudioStore, copier ports.AudioCopier, sharedCols []string, c promotionCandidate, prodUserID uuid.UUID, prodRef string) error {
	values, err := loadSharedRow(ctx, stagingPool, sharedCols, c.trackID)
	if err != nil {
		return err
	}

	userIDIdx := indexOf(sharedCols, "user_id")
	audioRefIdx := indexOf(sharedCols, "audio_ref")
	if userIDIdx < 0 || audioRefIdx < 0 {
		return errors.New("shared tracks columns are missing user_id or audio_ref")
	}
	values[userIDIdx] = prodUserID
	values[audioRefIdx] = prodRef

	if err := copier.Copy(ctx, c.audioRef, prodRef); err != nil {
		return fmt.Errorf("copy object: %w", err)
	}

	rowsAffected, err := insertPromotedTrack(ctx, prodPool, sharedCols, values)
	if err != nil {
		if delErr := store.Delete(ctx, prodRef); delErr != nil {
			slog.Warn("promote_staging_rollback_delete_failed", "ref", prodRef, "error", delErr)
		}
		return fmt.Errorf("insert prod track: %w", err)
	}
	if rowsAffected == 0 {
		if delErr := store.Delete(ctx, prodRef); delErr != nil {
			slog.Warn("promote_staging_race_delete_failed", "ref", prodRef, "error", delErr)
		}
	}
	return nil
}

func loadSharedRow(ctx context.Context, stagingPool *pgxpool.Pool, sharedCols []string, trackID uuid.UUID) ([]any, error) {
	quoted := make([]string, len(sharedCols))
	for i, col := range sharedCols {
		quoted[i] = `"` + col + `"`
	}
	query := fmt.Sprintf(`SELECT %s FROM tracks WHERE id = $1`, strings.Join(quoted, ", "))
	rows, err := stagingPool.Query(ctx, query, trackID)
	if err != nil {
		return nil, fmt.Errorf("query staging row: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, fmt.Errorf("staging track %s disappeared", trackID)
	}
	values, err := rows.Values()
	if err != nil {
		return nil, fmt.Errorf("read staging row values: %w", err)
	}
	return values, rows.Err()
}

func insertPromotedTrack(ctx context.Context, prodPool *pgxpool.Pool, sharedCols []string, values []any) (int64, error) {
	quoted := make([]string, len(sharedCols))
	placeholders := make([]string, len(sharedCols))
	for i, col := range sharedCols {
		quoted[i] = `"` + col + `"`
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	query := fmt.Sprintf(
		`INSERT INTO tracks (%s) VALUES (%s) ON CONFLICT (user_id, dedup_key) DO NOTHING`,
		strings.Join(quoted, ", "), strings.Join(placeholders, ", "))
	tag, err := prodPool.Exec(ctx, query, values...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func indexOf(cols []string, name string) int {
	for i, c := range cols {
		if c == name {
			return i
		}
	}
	return -1
}
