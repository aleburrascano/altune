package cache

import (
	"altune/go-api/internal/search/internal/domain"
	"context"
	"fmt"
	"sort"

	goredis "github.com/redis/go-redis/v9"
)

const vocabPrefixScanCap = 200

func (s *RedisVocabularyStore) SuggestByPrefix(
	ctx context.Context,
	prefix string,
	limit int,
) ([]domain.SpellingHintsEntry, error) {
	if s.disabled() {
		return nil, nil
	}
	return s.prefixSearch(ctx, prefix, limit)
}

func (s *RedisVocabularyStore) prefixSearch(
	ctx context.Context,
	prefix string,
	limit int,
) ([]domain.SpellingHintsEntry, error) {
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
	lo := "[" + normPrefix
	hi := "[" + normPrefix + "\xff"
	members, err := s.client.ZRangeArgs(ctx, goredis.ZRangeArgs{
		Key:    vocabLexKey,
		ByLex:  true,
		Start:  lo,
		Stop:   hi,
		Offset: 0,
		Count:  vocabPrefixScanCap,
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("read vocabulary terms with prefix %q: %w", normPrefix, err)
	}
	return members, nil
}

func (s *RedisVocabularyStore) topByScore(ctx context.Context) ([]string, error) {
	members, err := s.client.ZRangeArgs(ctx, goredis.ZRangeArgs{
		Key:     vocabTermsKey,
		ByScore: true,
		Rev:     true,
		Start:   "+inf",
		Stop:    "-inf",
		Offset:  0,
		Count:   100,
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("read top vocabulary terms: %w", err)
	}
	return members, nil
}

func (s *RedisVocabularyStore) membersToSortedEntries(
	ctx context.Context,
	members []string,
	limit int,
) []domain.SpellingHintsEntry {
	entries := make([]domain.SpellingHintsEntry, 0, len(members))
	for _, m := range members {
		norm, term, kind := decodeMember(m)
		if norm == "" {
			continue
		}
		entries = append(entries, domain.SpellingHintsEntry{
			Term:     term,
			TermNorm: norm,
			Kind:     domain.SpellingHintsKind(kind),
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

func (s *RedisVocabularyStore) attachScores(ctx context.Context, entries []domain.SpellingHintsEntry) {
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
