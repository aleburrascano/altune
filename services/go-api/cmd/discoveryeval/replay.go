package main

import (
	"context"
	"fmt"
	"os"

	discoveryEval "altune/go-api/internal/discovery/service/eval"
	"altune/go-api/internal/shared/config"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

func runReplay(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, redisClient *goredis.Client, opts options) error {
	if opts.corpusFile == "" {
		return fmt.Errorf("replay needs -corpus-file pointing at a behavioral corpus (see -mode corpus-build)")
	}
	corpus, err := discoveryEval.LoadBehavioralCorpus(opts.corpusFile)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "behavioral corpus: %d entries (%d positive, %d negative) from %s\n",
		len(corpus.Entries), len(corpus.Positives()), len(corpus.Negatives()), corpus.GeneratedFrom)

	searcher, drain := buildEvalSearcher(cfg, pool, redisClient)
	ranking := discoveryEval.BuildRanking(ctx, corpus, searcher)
	drain()

	score := discoveryEval.ReplayCorpus(corpus, ranking, opts.topK)
	if err := maybeWriteJSON(opts.jsonPath, score); err != nil {
		return err
	}
	fmt.Print(renderReplay(score))
	return nil
}
