package commands

import (
	"altune/go-api/cmd/api/commands/journey"
	"altune/go-api/internal/download"
	"altune/go-api/internal/shared/config"
	"context"
	"os"
)

func RunJourneyCheck(cfg *config.Config) error {
	return runJourneyCheckWithConfig(cfg)
}

func runJourneyCheckWithConfig(cfg *config.Config) error {
	ctx := context.Background()
	pool, err := openPool(ctx, cfg)
	if err != nil {
		return journey.StepFailed("search", err)
	}
	defer pool.Close()

	searcher, err := journey.RankingOnlySearcher(ctx, cfg, pool)
	if err != nil {
		return journey.StepFailed("search", err)
	}
	return journey.Run(ctx, searcher, download.YtDlpSearcher(cfg), os.Stdout)
}
