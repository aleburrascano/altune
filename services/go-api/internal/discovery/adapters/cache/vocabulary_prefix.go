package cache

import (
	"context"
	"sort"

	"altune/go-api/internal/discovery/domain"

	goredis "github.com/redis/go-redis/v9"
)

const vocabPrefixScanCap = 200

func (s *RedisVocabularyStore) SuggestByPrefix(
	ctx context.Context,
	prefix string,
	limit int,
) ([]domain.VocabularyEntry, error) {
	if s.disabled() {
		return nil, nil
	}
	return s.prefixSearch(ctx, prefix, limit)
}

func (s *RedisVocabularyStore) prefixSearch(
	ctx context.Context,
	prefix string,
	limit int,
) ([]domain.VocabularyEntry, error) {
	norm := s.normalizeTerm(prefix)
	members, err := s.lexRangeMembers(ctx, norm)
	if err != nil {
		return nil, err
	}
	return s.membersToSortedEntries(ctx, members, limit), nil
}

func (s *RedisVocabularyStore) lexRangeMembers(
	ctx context.Context,
	normPrefix string,
) ([]string, error) {
	if normPrefix == "" {
		return s.topByScore(ctx)
	}
	min := "[" + normPrefix
	max := "[" + normPrefix + "\xff"
	return s.client.ZRangeByLex(ctx, vocabLexKey, &goredis.ZRangeBy{
		Min:   min,
		Max:   max,
		Count: vocabPrefixScanCap,
	}).Result()
}

func (s *RedisVocabularyStore) topByScore(ctx context.Context) ([]string, error) {
	return s.client.ZRevRangeByScore(ctx, vocabTermsKey, &goredis.ZRangeBy{
		Min:   "-inf",
		Max:   "+inf",
		Count: 100,
	}).Result()
}

func (s *RedisVocabularyStore) membersToSortedEntries(
	ctx context.Context,
	members []string,
	limit int,
) []domain.VocabularyEntry {
	entries := make([]domain.VocabularyEntry, 0, len(members))
	for _, m := range members {
		norm, term, kind := decodeMember(m)
		if norm == "" {
			continue
		}
		entries = append(entries, domain.VocabularyEntry{
			Term:     term,
			TermNorm: norm,
			Kind:     domain.VocabularyKind(kind),
		})
	}
	s.attachScores(ctx, entries)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Popularity > entries[j].Popularity
	})
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	return entries
}

func (s *RedisVocabularyStore) attachScores(ctx context.Context, entries []domain.VocabularyEntry) {
	if len(entries) == 0 {
		return
	}
	members := make([]string, len(entries))
	for i := range entries {
		members[i] = encodeMember(entries[i].TermNorm, entries[i].Term, string(entries[i].Kind))
	}
	scores, err := s.client.ZMScore(ctx, vocabTermsKey, members...).Result()
	if err != nil || len(scores) != len(entries) {
		return
	}
	for i := range entries {
		entries[i].Popularity = int64(scores[i])
	}
}
