package commands

import (
	"altune/go-api/internal/catalog/adapters/storage"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

var errNoAudioStore = errors.New("no audio store configured (need MUSIC_DIR or OCI_S3_* env vars)")

func exitOnError(err error) {
	if err != nil {
		fmt.Printf("ERROR: %v\n", err)
		os.Exit(1)
	}
}

func openPool(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	if cfg.DatabaseURL == "" {
		return nil, errors.New("DATABASE_URL not set")
	}
	pool, err := database.NewPool(ctx, cfg.DatabaseURL, cfg.DBPoolMaxConns)
	if err != nil {
		return nil, fmt.Errorf("database connection failed: %w", err)
	}
	return pool, nil
}

func NewAudioStoreFromConfig(cfg *config.Config) (ports.AudioStore, error) {
	store, err := unscopedAudioStoreFromConfig(cfg)
	if err != nil || cfg.AudioKeyPrefix == "" {
		return store, err
	}
	return storage.NewScopedAudioStore(store, cfg.AudioKeyPrefix), nil
}

func unscopedAudioStoreFromConfig(cfg *config.Config) (ports.AudioStore, error) {
	if cfg.HasOCIS3() {
		store, err := storage.NewObjectStorageAudioStore(storage.ObjectStorageConfig{
			Endpoint:  cfg.OCIS3Endpoint,
			AccessKey: cfg.OCIS3AccessKey,
			SecretKey: cfg.OCIS3SecretKey,
			Bucket:    cfg.OCIS3Bucket,
			Region:    cfg.OCIS3Region,
		})
		if err == nil {
			return store, nil
		}
	}
	if cfg.MusicDir != "" {
		return storage.NewFilesystemAudioStore(cfg.MusicDir), nil
	}
	return nil, errNoAudioStore
}

func printSummary(heading string) {
	fmt.Printf("\n%s\n", strings.Repeat("=", 50))
	fmt.Println(heading)
}

func printDryRunHint() {
	fmt.Println("\n  Run with --execute to apply changes.")
}
