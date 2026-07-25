package main

import (
	"context"
	"fmt"
	"os"

	"altune/go-api/internal/app"
	"altune/go-api/internal/discovery/domain"
	discoveryEval "altune/go-api/internal/discovery/service/eval"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

const seedBatchSize = 500

func runCorrectionSeed(ctx context.Context, pool *pgxpool.Pool, redisClient *goredis.Client, opts options) error {
	vocab := app.BuildVocabularyStore(redisClient)
	if vocab == nil {
		return fmt.Errorf("correction-seed needs a vocabulary store (set REDIS_URL)")
	}

	entities, err := resolveEntities(ctx, pool, opts)
	if err != nil {
		return fmt.Errorf("load library: %w", err)
	}

	entries := seedEntries(entities)
	for start := 0; start < len(entries); start += seedBatchSize {
		end := min(start+seedBatchSize, len(entries))
		if err := vocab.BulkAdd(ctx, entries[start:end]); err != nil {
			return fmt.Errorf("seed vocabulary: %w", err)
		}
	}

	fmt.Fprintf(os.Stderr, "seeded %d vocabulary entries from %d entities\n", len(entries), len(entities))
	return nil
}

func seedEntries(entities []discoveryEval.LibraryEntity) []domain.VocabularyEntry {
	seen := map[string]struct{}{}
	entries := []domain.VocabularyEntry{}

	add := func(term string, kind domain.VocabularyKind) {
		if term == "" {
			return
		}
		key := string(kind) + "\n" + term
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		entries = append(entries, domain.VocabularyEntry{Term: term, Kind: kind, Popularity: 1})
	}

	for _, e := range entities {
		add(e.Artist, domain.VocabKindArtist)
		add(e.Title, domain.VocabKindTrack)
	}
	return entries
}
