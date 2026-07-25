package main

import (
	"context"
	"fmt"
	"os"
	"time"

	discoveryEval "altune/go-api/internal/discovery/service/eval"

	"github.com/jackc/pgx/v5/pgxpool"
)

func resolveEntities(ctx context.Context, pool *pgxpool.Pool, opts options) ([]discoveryEval.LibraryEntity, error) {
	if opts.corpusFile == "" {
		return loadLibraryEntities(ctx, pool, opts.limit, opts.random)
	}
	corpus, err := loadFrozenCorpus(opts)
	if err != nil {
		return nil, err
	}
	return truncate(corpus.Entities, opts.limit), nil
}

func resolveArtists(ctx context.Context, pool *pgxpool.Pool, opts options, limit int) ([]string, error) {
	if opts.corpusFile == "" {
		return loadDistinctArtists(ctx, pool, limit)
	}
	corpus, err := loadFrozenCorpus(opts)
	if err != nil {
		return nil, err
	}
	return truncate(corpus.Artists(), limit), nil
}

func resolveTerms(ctx context.Context, pool *pgxpool.Pool, opts options) ([]string, error) {
	if opts.corpusFile == "" {
		return loadLibraryTerms(ctx, pool, opts.limit)
	}
	corpus, err := loadFrozenCorpus(opts)
	if err != nil {
		return nil, err
	}
	return truncate(corpus.Terms(), opts.limit), nil
}

func loadFrozenCorpus(opts options) (discoveryEval.LibraryCorpus, error) {
	if opts.random {
		return discoveryEval.LibraryCorpus{}, fmt.Errorf("-random cannot be combined with -corpus-file: a frozen corpus is deterministic by definition")
	}
	corpus, err := discoveryEval.LoadLibraryCorpus(opts.corpusFile)
	if err != nil {
		return discoveryEval.LibraryCorpus{}, err
	}
	fmt.Fprintf(os.Stderr, "frozen corpus: %d entities, generated %s\n", len(corpus.Entities), corpus.GeneratedAt)
	return corpus, nil
}

func truncate[T any](in []T, limit int) []T {
	if limit > 0 && len(in) > limit {
		return in[:limit]
	}
	return in
}

func runCorpusSnapshot(ctx context.Context, pool *pgxpool.Pool, opts options) error {
	if opts.corpusFile == "" {
		return fmt.Errorf("corpus-snapshot needs -corpus-file to know where to write")
	}

	entities, err := loadLibraryEntities(ctx, pool, opts.limit, false)
	if err != nil {
		return fmt.Errorf("load library: %w", err)
	}
	if len(entities) == 0 {
		return fmt.Errorf("refusing to write an empty corpus snapshot")
	}

	corpus := discoveryEval.NewLibraryCorpus(time.Now().UTC().Format("2006-01-02"), entities)
	if err := corpus.Save(opts.corpusFile); err != nil {
		return err
	}

	fmt.Printf("# Discovery eval corpus snapshot — %s\n\n", corpus.GeneratedAt)
	fmt.Printf("- Entities: %d\n", len(corpus.Entities))
	fmt.Printf("- Distinct artists: %d\n", len(corpus.Artists()))
	fmt.Printf("- Distinct terms: %d\n", len(corpus.Terms()))
	fmt.Printf("- Written to: %s\n", opts.corpusFile)
	return nil
}
