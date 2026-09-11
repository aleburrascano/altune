package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"altune/go-api/internal/catalog/adapters/storage"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var errNoAudioStore = errors.New("no audio store configured (need MUSIC_DIR or OCI_S3_* env vars)")

func mustOpenPool(ctx context.Context, cfg *config.Config) *pgxpool.Pool {
	if cfg.DatabaseURL == "" {
		fmt.Println("ERROR: DATABASE_URL not set")
		os.Exit(1)
	}
	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Printf("ERROR: database connection failed: %v\n", err)
		os.Exit(1)
	}
	return pool
}

func NewAudioStoreFromConfig(cfg *config.Config) (ports.AudioStore, error) {
	if cfg.HasOCIS3() {
		store, err := storage.NewObjectStorageAudioStore(
			cfg.OCIS3Endpoint, cfg.OCIS3AccessKey, cfg.OCIS3SecretKey,
			cfg.OCIS3Bucket, cfg.OCIS3Region,
		)
		if err == nil {
			return store, nil
		}
	}
	if cfg.MusicDir != "" {
		return storage.NewFilesystemAudioStore(cfg.MusicDir), nil
	}
	return nil, errNoAudioStore
}

func mustAudioStore(cfg *config.Config) ports.AudioStore {
	store, err := NewAudioStoreFromConfig(cfg)
	if err != nil {
		fmt.Printf("ERROR: %v\n", err)
		os.Exit(1)
	}
	return store
}

type ReadyTrack struct {
	Id       domain.TrackId
	UserId   shared.UserId
	Title    string
	Artist   string
	AudioRef string
}

func loadReadyTracks(ctx context.Context, pool *pgxpool.Pool, pred, orderBy string) []ReadyTrack {
	query := `SELECT id, user_id, title, artist, audio_ref
		FROM tracks
		WHERE acquisition_status = 'ready' AND audio_ref IS NOT NULL` + pred + orderBy
	rows, err := pool.Query(ctx, query)
	if err != nil {
		fmt.Printf("ERROR: query failed: %v\n", err)
		os.Exit(1)
	}
	defer rows.Close()

	var tracks []ReadyTrack
	for rows.Next() {
		var id, userId uuid.UUID
		var t ReadyTrack
		if err := rows.Scan(&id, &userId, &t.Title, &t.Artist, &t.AudioRef); err != nil {
			fmt.Printf("ERROR: scan failed: %v\n", err)
			os.Exit(1)
		}
		t.Id = domain.TrackIdFromUUID(id)
		t.UserId = shared.NewUserId(userId)
		tracks = append(tracks, t)
	}
	return tracks
}

func markTrackFailed(ctx context.Context, pool *pgxpool.Pool, id domain.TrackId, userId shared.UserId, reason string) error {
	t := &domain.Track{ID: id, UserId: userId}
	if err := t.MarkFailed(reason); err != nil {
		return err
	}
	_, err := pool.Exec(ctx,
		`UPDATE tracks SET acquisition_status = $3, failure_reason = $4, audio_ref = $5
			WHERE id = $1 AND user_id = $2`,
		id.UUID(), userId.UUID(), t.AcquisitionStatus.String(), t.FailureReason, t.AudioRef)
	return err
}

func printSummary(heading string) {
	fmt.Printf("\n%s\n", strings.Repeat("=", 50))
	fmt.Println(heading)
}

func printDryRunHint() {
	fmt.Println("\n  Run with --execute to apply changes.")
}
