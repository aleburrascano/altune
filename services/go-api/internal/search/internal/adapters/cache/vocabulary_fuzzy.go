package cache

import (
	"altune/go-api/internal/search/internal/adapters/cache/spellingmatch"
	"altune/go-api/internal/search/internal/domain"
	"context"
	"encoding/json"
	"fmt"
	"sort"

	goredis "github.com/redis/go-redis/v9"
)

func (s *RedisVocabularyStore) FindClosest(
	ctx context.Context,
	query string,
	limit int,
) ([]domain.SpellingHintsEntry, error) {
	if s.disabled() {
		return nil, nil
	}
	return s.fuzzySearch(ctx, query, limit)
}

func (s *RedisVocabularyStore) fuzzySearch(
	ctx context.Context,
	query string,
	limit int,
) ([]domain.SpellingHintsEntry, error) {
	norm := s.normalizeTerm(query)
	q := spellingmatch.NewQuery(norm)
	if len(q.Trigrams()) == 0 {
		return nil, nil
	}
	candidates, err := s.trigramCandidates(ctx, q.Trigrams())
	if err != nil {
		return nil, err
	}

	var phoneticSet map[string]bool
	if code := s.metaphone(norm); code != "" {
		phoneticSet = s.metaphoneCandidatesLogged(ctx, code)
		for norm := range phoneticSet {
			if _, exists := candidates[norm]; !exists {
				candidates[norm] = 0
			}
		}
	}

	return s.topMatchingEntries(ctx, candidates, q, limit, phoneticSet)
}

func (s *RedisVocabularyStore) metaphoneCandidatesLogged(ctx context.Context, code string) map[string]bool {
	set, err := s.metaphoneCandidates(ctx, code)
	if err != nil {
		s.signal.failure(ctx, kindVocab, opGet, err)
	}
	return set
}

const vocabPhoneticCandidateCap = 64

const vocabTrigramSetReadCap = 512

func (s *RedisVocabularyStore) metaphoneCandidates(
	ctx context.Context,
	code string,
) (map[string]bool, error) {
	members, err := s.scanCapped(ctx, vocabMetaPrefix+code, 0, vocabPhoneticCandidateCap)
	if err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(members))
	for _, m := range members {
		result[m] = true
	}
	return result, nil
}

func (s *RedisVocabularyStore) scanCapped(
	ctx context.Context,
	key string,
	cursor uint64,
	readCap int,
) ([]string, error) {
	var members []string
	for len(members) < readCap {
		page, next, err := s.client.SScan(ctx, key, cursor, "", int64(readCap)).Result()
		if err != nil {
			return nil, fmt.Errorf("scan vocabulary set %s: %w", key, err)
		}
		members = append(members, page...)
		if next == 0 {
			break
		}
		cursor = next
	}
	return capMembers(members, readCap), nil
}

func capMembers(members []string, readCap int) []string {
	if len(members) > readCap {
		return members[:readCap]
	}
	return members
}

const vocabTrigramLookupCap = 64

func trigramLookupKeys(queryTrigrams []string) []string {
	if len(queryTrigrams) > vocabTrigramLookupCap {
		queryTrigrams = queryTrigrams[:vocabTrigramLookupCap]
	}
	keys := make([]string, 0, len(queryTrigrams))
	for _, tri := range queryTrigrams {
		keys = append(keys, vocabTriPrefix+tri)
	}
	return keys
}

func (s *RedisVocabularyStore) trigramCandidates(
	ctx context.Context,
	queryTrigrams []string,
) (map[string]int, error) {
	keys := trigramLookupKeys(queryTrigrams)
	cmds := make([]*goredis.ScanCmd, len(keys))
	_, pipeErr := s.client.Pipelined(ctx, func(pipe goredis.Pipeliner) error {
		for i, key := range keys {
			cmds[i] = pipe.SScan(ctx, key, 0, "", vocabTrigramSetReadCap)
		}
		return nil
	})
	if pipeErr != nil {
		s.signal.failure(ctx, kindVocab, opGet, pipeErr)
	}
	candidates := map[string]int{}
	for i, cmd := range cmds {
		for _, m := range s.trigramMembers(ctx, keys[i], cmd) {
			candidates[m]++
		}
	}
	return topSharedCandidates(candidates), nil
}

func (s *RedisVocabularyStore) trigramMembers(ctx context.Context, key string, first *goredis.ScanCmd) []string {
	page, cursor := first.Val()
	if cursor == 0 || len(page) >= vocabTrigramSetReadCap {
		return capMembers(page, vocabTrigramSetReadCap)
	}
	rest, err := s.scanCapped(ctx, key, cursor, vocabTrigramSetReadCap-len(page))
	if err != nil {
		s.signal.failure(ctx, kindVocab, opGet, err)
		return page
	}
	return append(page, rest...)
}

type sharedCandidate struct {
	norm   string
	shared int
}

const vocabFuzzyPrefilterCap = 256

func topSharedCandidates(candidates map[string]int) map[string]int {
	if len(candidates) <= vocabFuzzyPrefilterCap {
		return candidates
	}
	top := make(map[string]int, vocabFuzzyPrefilterCap)
	for _, c := range rankedByShared(candidates)[:vocabFuzzyPrefilterCap] {
		top[c.norm] = c.shared
	}
	return top
}

func rankedByShared(candidates map[string]int) []sharedCandidate {
	ranked := make([]sharedCandidate, 0, len(candidates))
	for norm, shared := range candidates {
		ranked = append(ranked, sharedCandidate{norm: norm, shared: shared})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].shared > ranked[j].shared })
	return ranked
}

type scoredNorm struct {
	norm  string
	score float64
}

type fuzzyCandidate struct {
	entry domain.SpellingHintsEntry
	score float64
}

func (s *RedisVocabularyStore) topMatchingEntries(
	ctx context.Context,
	candidates map[string]int,
	q spellingmatch.Query,
	limit int,
	phoneticSet map[string]bool,
) ([]domain.SpellingHintsEntry, error) {
	survivors := survivingNorms(candidates, q, phoneticSet)
	return topFuzzyCandidates(s.loadEntries(ctx, survivors), limit), nil
}

func survivingNorms(
	candidates map[string]int,
	q spellingmatch.Query,
	phoneticSet map[string]bool,
) []scoredNorm {
	survivors := make([]scoredNorm, 0, len(candidates))
	for norm, shared := range candidates {
		score, matched := q.Score(norm, shared, phoneticSet[norm])
		if !matched {
			continue
		}
		survivors = append(survivors, scoredNorm{norm: norm, score: score})
	}
	return survivors
}

func (s *RedisVocabularyStore) loadEntries(
	ctx context.Context,
	survivors []scoredNorm,
) []fuzzyCandidate {
	if len(survivors) == 0 {
		return nil
	}
	keys := make([]string, len(survivors))
	for i, survivor := range survivors {
		keys[i] = vocabEntryPfx + survivor.norm
	}
	raw, err := s.client.MGet(ctx, keys...).Result()
	if err != nil || len(raw) != len(survivors) {
		return nil
	}
	return decodeFuzzyCandidates(survivors, raw)
}

func decodeFuzzyCandidates(survivors []scoredNorm, raw []any) []fuzzyCandidate {
	loaded := make([]fuzzyCandidate, 0, len(survivors))
	for i, survivor := range survivors {
		entry, decoded := decodeEntry(raw[i])
		if !decoded {
			continue
		}
		entry.TermNorm = survivor.norm
		loaded = append(loaded, fuzzyCandidate{entry: entry, score: survivor.score})
	}
	return loaded
}

func decodeEntry(raw any) (domain.SpellingHintsEntry, bool) {
	blob, isString := raw.(string)
	if !isString {
		return domain.SpellingHintsEntry{}, false
	}
	var stored vocabEntryData
	if err := json.Unmarshal([]byte(blob), &stored); err != nil {
		return domain.SpellingHintsEntry{}, false
	}
	return domain.SpellingHintsEntry{
		Term:       stored.Term,
		Kind:       domain.SpellingHintsKind(stored.Kind),
		Popularity: stored.Popularity,
	}, true
}

func topFuzzyCandidates(scored []fuzzyCandidate, limit int) []domain.SpellingHintsEntry {
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})
	if limit > 0 && len(scored) > limit {
		scored = scored[:limit]
	}
	results := make([]domain.SpellingHintsEntry, len(scored))
	for i, c := range scored {
		c.entry.MatchScore = c.score
		results[i] = c.entry
	}
	return results
}
