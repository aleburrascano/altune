package cache

import (
	"context"
	"encoding/json"
	"fmt"

	"altune/go-api/internal/discovery/domain"

	goredis "github.com/redis/go-redis/v9"
)

func (s *RedisVocabularyStore) Add(ctx context.Context, entry domain.VocabularyEntry) error {
	if s.client == nil {
		return nil
	}
	return s.indexEntry(ctx, entry)
}

func (s *RedisVocabularyStore) BulkAdd(ctx context.Context, entries []domain.VocabularyEntry) error {
	if s.client == nil || len(entries) == 0 {
		return nil
	}
	pipe := s.client.Pipeline()
	for _, e := range entries {
		addEntryToPipeline(pipe, ctx, s.buildNorm(e), e, s.metaphone)
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (s *RedisVocabularyStore) indexEntry(
	ctx context.Context,
	entry domain.VocabularyEntry,
) error {
	norm := s.buildNorm(entry)
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

func (s *RedisVocabularyStore) Trim(ctx context.Context, maxEntries int) error {
	if s.client == nil || maxEntries <= 0 {
		return nil
	}
	count, err := s.client.ZCard(ctx, vocabTermsKey).Result()
	if err != nil {
		return fmt.Errorf("vocab trim: card: %w", err)
	}
	overflow := int(count) - maxEntries
	if overflow <= 0 {
		return nil
	}
	members, err := s.client.ZRange(ctx, vocabTermsKey, 0, int64(overflow-1)).Result()
	if err != nil {
		return fmt.Errorf("vocab trim: range: %w", err)
	}
	pipe := s.client.Pipeline()
	for _, member := range members {
		norm, _, _ := decodeMember(member)
		if norm == "" {
			continue
		}
		s.queueEvict(ctx, pipe, norm, member)
	}
	if _, err := pipe.Exec(ctx); err != nil {
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
