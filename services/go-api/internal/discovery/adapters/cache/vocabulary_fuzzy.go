package cache

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"encoding/json"
	"sort"

	goredis "github.com/redis/go-redis/v9"
)

func (s *RedisVocabularyStore) FindClosest(
	ctx context.Context,
	query string,
	limit int,
) ([]domain.VocabularyEntry, error) {
	if s.disabled() {
		return nil, nil
	}
	return s.fuzzySearch(ctx, query, limit)
}

func (s *RedisVocabularyStore) fuzzySearch(
	ctx context.Context,
	query string,
	limit int,
) ([]domain.VocabularyEntry, error) {
	norm := s.normalizeTerm(query)
	queryTrigrams := trigrams(norm)
	if len(queryTrigrams) == 0 {
		return nil, nil
	}
	candidates, err := s.trigramCandidates(ctx, queryTrigrams)
	if err != nil {
		return nil, err
	}

	var phoneticSet map[string]bool
	if s.metaphone != nil {
		code := s.metaphone(norm)
		if code != "" {
			phoneticSet = s.metaphoneCandidatesLogged(ctx, code)
			for norm := range phoneticSet {
				if _, exists := candidates[norm]; !exists {
					candidates[norm] = 0
				}
			}
		}
	}

	return s.topMatchingEntries(ctx, candidates, queryTrigrams, norm, limit, phoneticSet)
}

func (s *RedisVocabularyStore) metaphoneCandidatesLogged(ctx context.Context, code string) map[string]bool {
	set, err := s.metaphoneCandidates(ctx, code)
	if err != nil {
		s.signal.failure(ctx, kindVocab, opGet, err)
	}
	return set
}

// vocabPhoneticCandidateCap bounds the phonetic bucket one lookup keeps. Its
// members bypass the edit-distance filter, so a crowded metaphone code would
// otherwise put its whole bucket into the entry load.
const vocabPhoneticCandidateCap = 64

func (s *RedisVocabularyStore) metaphoneCandidates(
	ctx context.Context,
	code string,
) (map[string]bool, error) {
	members, err := s.client.SMembers(ctx, vocabMetaPrefix+code).Result()
	if err != nil {
		return nil, err
	}
	if len(members) > vocabPhoneticCandidateCap {
		members = members[:vocabPhoneticCandidateCap]
	}
	result := make(map[string]bool, len(members))
	for _, m := range members {
		result[m] = true
	}
	return result, nil
}

// vocabTrigramLookupCap bounds the trigram sets one fuzzy lookup reads, the
// fuzzy counterpart of vocabPrefixScanCap. A real title or artist name stays
// well under it (64 trigrams = a 66-rune string); an oversized or adversarial
// query is truncated rather than fanned out into one Redis read per trigram.
const vocabTrigramLookupCap = 64

// trigramLookupKeys maps query trigrams to their set keys, keeping at most
// vocabTrigramLookupCap of them.
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

// trigramCandidates reads the capped trigram sets in a single pipelined round
// trip. A failed set read is skipped so one bad key degrades, not fails, the
// lookup.
func (s *RedisVocabularyStore) trigramCandidates(
	ctx context.Context,
	queryTrigrams []string,
) (map[string]int, error) {
	keys := trigramLookupKeys(queryTrigrams)
	cmds := make([]*goredis.StringSliceCmd, len(keys))
	_, pipeErr := s.client.Pipelined(ctx, func(pipe goredis.Pipeliner) error {
		for i, key := range keys {
			cmds[i] = pipe.SMembers(ctx, key)
		}
		return nil
	})
	if pipeErr != nil {
		s.signal.failure(ctx, kindVocab, opGet, pipeErr)
	}
	candidates := map[string]int{}
	for _, cmd := range cmds {
		for _, m := range cmd.Val() {
			candidates[m]++
		}
	}
	return topSharedCandidates(candidates), nil
}

type sharedCandidate struct {
	norm   string
	shared int
}

// vocabFuzzyPrefilterCap bounds how many trigram candidates one lookup scores
// and loads. A common trigram such as "the" holds thousands of a 50k-entry
// vocabulary; a real match shares most of the query's trigrams, so the tail that
// shares one is dropped before it costs any work.
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
	entry domain.VocabularyEntry
	score float64
}

func (s *RedisVocabularyStore) topMatchingEntries(
	ctx context.Context,
	candidates map[string]int,
	queryTrigrams []string,
	queryNorm string,
	limit int,
	phoneticSet map[string]bool,
) ([]domain.VocabularyEntry, error) {
	survivors := survivingNorms(candidates, queryTrigrams, queryNorm, phoneticSet)
	return topFuzzyCandidates(s.loadEntries(ctx, survivors), limit), nil
}

// survivingNorms scores straight from the candidate key: everything the score
// needs is in the norm itself, so the edit-distance filter runs before any entry
// is fetched rather than after.
func survivingNorms(
	candidates map[string]int,
	queryTrigrams []string,
	queryNorm string,
	phoneticSet map[string]bool,
) []scoredNorm {
	survivors := make([]scoredNorm, 0, len(candidates))
	for norm, shared := range candidates {
		score, matched := matchScore(norm, shared, queryTrigrams, queryNorm, phoneticSet[norm])
		if !matched {
			continue
		}
		survivors = append(survivors, scoredNorm{norm: norm, score: score})
	}
	return survivors
}

func matchScore(
	norm string,
	shared int,
	queryTrigrams []string,
	queryNorm string,
	isPhonetic bool,
) (float64, bool) {
	dist := textnorm.LevenshteinDistance(queryNorm, norm)
	if dist > maxLevenshtein(queryNorm) && !isPhonetic {
		return 0, false
	}
	return domain.VocabularyMatchScore(
		jaccardCoefficient(shared, len(queryTrigrams), len(trigrams(norm))),
		levenshteinSimilarity(queryNorm, norm, dist),
		phoneticScore(isPhonetic),
		lengthSimilarity(queryNorm, norm),
	), true
}

// loadEntries fetches the survivors in one MGET. A norm the index still holds
// but whose entry key has expired is dropped, never returned blank.
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

func decodeEntry(raw any) (domain.VocabularyEntry, bool) {
	blob, isString := raw.(string)
	if !isString {
		return domain.VocabularyEntry{}, false
	}
	var data vocabEntryData
	if err := json.Unmarshal([]byte(blob), &data); err != nil {
		return domain.VocabularyEntry{}, false
	}
	return domain.VocabularyEntry{
		Term:       data.Term,
		Kind:       domain.VocabularyKind(data.Kind),
		Popularity: data.Popularity,
	}, true
}

func topFuzzyCandidates(scored []fuzzyCandidate, limit int) []domain.VocabularyEntry {
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})
	if limit > 0 && len(scored) > limit {
		scored = scored[:limit]
	}
	results := make([]domain.VocabularyEntry, len(scored))
	for i, c := range scored {
		c.entry.MatchScore = c.score
		results[i] = c.entry
	}
	return results
}

func jaccardCoefficient(shared, totalA, totalB int) float64 {
	union := totalA + totalB - shared
	if union == 0 {
		return 0
	}
	return float64(shared) / float64(union)
}

func maxLevenshtein(query string) int {
	n := len([]rune(query))
	if n <= 4 {
		return 1
	}
	if n <= 8 {
		return 2
	}
	return 3
}

func levenshteinSimilarity(a, b string, dist int) float64 {
	maxLen := len([]rune(a))
	if bl := len([]rune(b)); bl > maxLen {
		maxLen = bl
	}
	if maxLen == 0 {
		return 0.0
	}
	return 1.0 - float64(dist)/float64(maxLen)
}

func phoneticScore(inPhoneticSet bool) float64 {
	if inPhoneticSet {
		return 1.0
	}
	return 0.0
}

func lengthSimilarity(a, b string) float64 {
	aLen := len([]rune(a))
	bLen := len([]rune(b))
	if aLen == 0 && bLen == 0 {
		return 0.0
	}
	bigger := aLen
	if bLen > bigger {
		bigger = bLen
	}
	diff := aLen - bLen
	if diff < 0 {
		diff = -diff
	}
	return 1.0 - float64(diff)/float64(bigger)
}
