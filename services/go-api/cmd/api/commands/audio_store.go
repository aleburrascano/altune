package commands

import (
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

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

func printSummary(heading string) {
	fmt.Printf("\n%s\n", strings.Repeat("=", 50))
	fmt.Println(heading)
}

func printDryRunHint() {
	fmt.Println("\n  Run with --execute to apply changes.")
}
