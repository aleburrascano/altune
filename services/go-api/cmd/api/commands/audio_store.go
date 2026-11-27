package commands

import (
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

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

const summaryRuleWidth = 50

func printSummary(heading string) {
	fmt.Printf("\n%s\n", strings.Repeat("=", summaryRuleWidth))
	fmt.Println(heading)
}

func printDryRunHint() {
	fmt.Println("\n  Run with --execute to apply changes.")
}
