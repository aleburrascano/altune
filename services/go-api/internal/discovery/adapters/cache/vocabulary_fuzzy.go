package cache

import (
	"context"
	"encoding/json"
	"sort"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
)

func (s *RedisVocabularyStore) FindClosest(
	ctx context.Context,
	query string,
	limit int,
) ([]domain.VocabularyEntry, error) {
	if s.client == nil {
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
			phoneticSet, _ = s.metaphoneCandidates(ctx, code)
			for norm := range phoneticSet {
				if _, exists := candidates[norm]; !exists {
					candidates[norm] = 0
				}
			}
		}
	}

	return s.scoreCandidatesWithPhonetic(ctx, candidates, queryTrigrams, norm, limit, phoneticSet)
}

func (s *RedisVocabularyStore) metaphoneCandidates(
	ctx context.Context,
	code string,
) (map[string]bool, error) {
	members, err := s.client.SMembers(ctx, vocabMetaPrefix+code).Result()
	if err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(members))
	for _, m := range members {
		result[m] = true
	}
	return result, nil
}

func (s *RedisVocabularyStore) trigramCandidates(
	ctx context.Context,
	queryTrigrams []string,
) (map[string]int, error) {
	candidates := map[string]int{}
	for _, tri := range queryTrigrams {
		members, err := s.client.SMembers(ctx, vocabTriPrefix+tri).Result()
		if err != nil {
			continue
		}
		for _, m := range members {
			candidates[m]++
		}
	}
	return candidates, nil
}

type fuzzyCandidate struct {
	entry   domain.VocabularyEntry
	jaccard float64
}

func (s *RedisVocabularyStore) scoreCandidatesWithPhonetic(
	ctx context.Context,
	candidates map[string]int,
	queryTrigrams []string,
	queryNorm string,
	limit int,
	phoneticSet map[string]bool,
) ([]domain.VocabularyEntry, error) {
	scored := make([]fuzzyCandidate, 0, len(candidates))
	for norm, shared := range candidates {
		entry, err := s.loadEntry(ctx, norm)
		if err != nil {
			continue
		}
		candTrigrams := trigrams(norm)
		jaccard := jaccardCoefficient(shared, len(queryTrigrams), len(candTrigrams))

		dist := textnorm.LevenshteinDistance(queryNorm, norm)
		maxDist := maxLevenshtein(queryNorm)
		if dist > maxDist && !phoneticSet[norm] {
			continue
		}

		levSim := levenshteinSimilarity(queryNorm, norm, dist)

		phonetic := phoneticScore(phoneticSet[norm])

		lengthSim := lengthSimilarity(queryNorm, norm)

		combined := domain.VocabularyMatchScore(jaccard, levSim, phonetic, lengthSim)

		entry.TermNorm = norm
		scored = append(scored, fuzzyCandidate{entry: entry, jaccard: combined})
	}
	return topFuzzyCandidates(scored, limit), nil
}

func (s *RedisVocabularyStore) loadEntry(
	ctx context.Context,
	norm string,
) (domain.VocabularyEntry, error) {
	raw, err := s.client.Get(ctx, vocabEntryPfx+norm).Result()
	if err != nil {
		return domain.VocabularyEntry{}, err
	}
	var data vocabEntryData
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return domain.VocabularyEntry{}, err
	}
	return domain.VocabularyEntry{
		Term:       data.Term,
		Kind:       domain.VocabularyKind(data.Kind),
		Popularity: data.Popularity,
	}, nil
}

func topFuzzyCandidates(scored []fuzzyCandidate, limit int) []domain.VocabularyEntry {
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].jaccard > scored[j].jaccard
	})
	if limit > 0 && len(scored) > limit {
		scored = scored[:limit]
	}
	results := make([]domain.VocabularyEntry, len(scored))
	for i, c := range scored {
		c.entry.MatchScore = c.jaccard
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
