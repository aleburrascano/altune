package commands

import (
	"altune/go-api/cmd/api/commands/journey"
	"altune/go-api/internal/app/downloadsetup"
	"altune/go-api/internal/shared/config"
	"context"
	"os"
)

func RunJourneyCheck(cfg *config.Config) {
	exitOnError(runJourneyCheckWithConfig(cfg))
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
	return journey.Run(ctx, searcher, downloadsetup.YtDlpSearcher(cfg), os.Stdout)
}
