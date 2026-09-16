package cache

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	goredis "github.com/redis/go-redis/v9"
)

func (s *RedisVocabularyStore) Add(ctx context.Context, entry domain.VocabularyEntry) error {
	if s.disabled() {
		return nil
	}
	return s.indexEntry(ctx, entry)
}

func (s *RedisVocabularyStore) BulkAdd(ctx context.Context, entries []domain.VocabularyEntry) error {
	if s.disabled() || len(entries) == 0 {
		return nil
	}
	pipe := s.client.Pipeline()
	for _, e := range entries {
		norm := s.buildNorm(e)
		if domain.IsIndexableVocabularyTerm(e.Term, norm) {
			addEntryToPipeline(pipe, ctx, norm, e, s.metaphone)
		}
	}
	if pipe.Len() == 0 {
		return nil
	}
	_, err := pipe.Exec(ctx)
	return err
}

// indexEntry silently skips an oversized term: it is not an error worth
// surfacing per request, just a write the shared index refuses.
func (s *RedisVocabularyStore) indexEntry(
	ctx context.Context,
	entry domain.VocabularyEntry,
) error {
	norm := s.buildNorm(entry)
	if !domain.IsIndexableVocabularyTerm(entry.Term, norm) {
		return nil
	}
	pipe := s.client.Pipeline()
	addEntryToPipeline(pipe, ctx, norm, entry, s.metaphone)
	_, err := pipe.Exec(ctx)
	return err
}

func addEntryToPipeline(
	pipe goredis.Pipeliner,
	ctx context.Context,
	norm string,
	entry domain.VocabularyEntry,
	metaphone MetaphoneFunc,
) {
	member := encodeMember(norm, entry.Term, string(entry.Kind))
	pipe.ZAdd(ctx, vocabTermsKey, goredis.Z{
		Score:  float64(entry.Popularity),
		Member: member,
	})
	pipe.ZAdd(ctx, vocabLexKey, goredis.Z{Score: 0, Member: member})
	entryJSON, _ := json.Marshal(vocabEntryData{
		Term:       entry.Term,
		Kind:       string(entry.Kind),
		Popularity: entry.Popularity,
	})
	pipe.Set(ctx, vocabEntryPfx+norm, entryJSON, vocabEntryTTL)
	for _, tri := range trigrams(norm) {
		pipe.SAdd(ctx, vocabTriPrefix+tri, norm)
	}
	if metaphone != nil {
		code := metaphone(norm)
		if code != "" {
			pipe.SAdd(ctx, vocabMetaPrefix+code, norm)
		}
	}
}

// vocabTrimAttempts bounds how often Trim re-reads after a concurrent write
// to the terms ZSET invalidates its overflow snapshot.
const vocabTrimAttempts = 5

// Trim evicts the lowest-popularity overflow. The overflow read and the evict
// run under WATCH on the terms ZSET: Add/BulkAdd write that key first, so a
// term re-added between the read and the evict aborts the EXEC and Trim
// re-reads instead of evicting a term the fresh write just kept.
func (s *RedisVocabularyStore) Trim(ctx context.Context, maxEntries int) error {
	if s.disabled() || maxEntries <= 0 {
		return nil
	}
	for range vocabTrimAttempts {
		err := s.client.Watch(ctx, func(tx *goredis.Tx) error {
			return s.trimTx(ctx, tx, maxEntries)
		}, vocabTermsKey)
		if !errors.Is(err, goredis.TxFailedErr) {
			return err
		}
	}
	return fmt.Errorf("vocab trim: terms kept changing across %d attempts: %w", vocabTrimAttempts, goredis.TxFailedErr)
}

func (s *RedisVocabularyStore) trimTx(ctx context.Context, tx *goredis.Tx, maxEntries int) error {
	count, err := tx.ZCard(ctx, vocabTermsKey).Result()
	if err != nil {
		return fmt.Errorf("vocab trim: card: %w", err)
	}
	overflow := int(count) - maxEntries
	if overflow <= 0 {
		return nil
	}
	members, err := tx.ZRange(ctx, vocabTermsKey, 0, int64(overflow-1)).Result()
	if err != nil {
		return fmt.Errorf("vocab trim: range: %w", err)
	}
	_, err = tx.TxPipelined(ctx, func(pipe goredis.Pipeliner) error {
		for _, member := range members {
			norm, _, _ := decodeMember(member)
			if norm == "" {
				continue
			}
			s.queueEvict(ctx, pipe, norm, member)
		}
		return nil
	})
	if errors.Is(err, goredis.TxFailedErr) {
		return err
	}
	if err != nil {
		return fmt.Errorf("vocab trim: evict: %w", err)
	}
	return nil
}

func (s *RedisVocabularyStore) queueEvict(ctx context.Context, pipe goredis.Pipeliner, norm, member string) {
	pipe.ZRem(ctx, vocabTermsKey, member)
	pipe.ZRem(ctx, vocabLexKey, member)
	for _, tri := range trigrams(norm) {
		pipe.SRem(ctx, vocabTriPrefix+tri, norm)
	}
	if s.metaphone != nil {
		if code := s.metaphone(norm); code != "" {
			pipe.SRem(ctx, vocabMetaPrefix+code, norm)
		}
	}
	pipe.Del(ctx, vocabEntryPfx+norm)
}
