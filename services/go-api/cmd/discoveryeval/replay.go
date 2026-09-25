package main

import (
	"altune/go-api/internal/shared/config"
	"context"
	"fmt"
	"os"

	discoveryEval "altune/go-api/internal/discovery/service/eval"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

func runReplay(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, redisClient *goredis.Client, opts options) error {
	if opts.corpusFile == "" {
		return fmt.Errorf("replay needs -corpus-file pointing at a behavioral corpus (see -mode corpus-build)")
	}
	if opts.topK <= 0 {
		return fmt.Errorf("replay needs -top-k > 0, got %d", opts.topK)
	}
	corpus, err := discoveryEval.LoadBehavioralCorpus(opts.corpusFile)
	if err != nil {
		return err
	}
	if len(corpus.Entries) == 0 {
		return fmt.Errorf("behavioral corpus %q has no entries", opts.corpusFile)
	}
	fmt.Fprintf(os.Stderr, "behavioral corpus: %d entries (%d positive, %d negative) from %s\n",
		len(corpus.Entries), len(corpus.Positives()), len(corpus.Negatives()), corpus.GeneratedFrom)

	searcher, drain := buildEvalSearcher(cfg, pool, redisClient)
	ranking, failed := discoveryEval.BuildRankingCountingFailures(ctx, corpus, searcher)
	drain()

	score := discoveryEval.ReplayCorpus(corpus, ranking, opts.topK)
	score.FailedQueries = failed
	if failed > 0 {
		fmt.Fprintf(os.Stderr, "WARNING: %d queries failed to search; their entries are scored as not found / not leaked\n", failed)
	}
	if err := maybeWriteJSON(opts.jsonPath, score); err != nil {
		return err
	}
	fmt.Print(renderReplay(score))
	return nil
}
