package cache

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/music"
)

const trigramRunes = 3

func Trigrams(norm string) []string {
	runes := []rune(norm)
	if len(runes) == 0 {
		return nil
	}
	if len(runes) < trigramRunes {
		return []string{norm}
	}
	out := make([]string, 0, len(runes))
	for i := 0; i <= len(runes)-trigramRunes; i++ {
		out = append(out, string(runes[i:i+trigramRunes]))
	}
	return out
}

type Query struct {
	norm     string
	trigrams []string
}

func NewQuery(norm string) Query {
	return Query{norm: norm, trigrams: Trigrams(norm)}
}

func (q Query) Trigrams() []string { return q.trigrams }

func (q Query) Score(candidate string, sharedTrigrams int) (float64, bool) {
	dist := music.LevenshteinDistance(q.norm, candidate)
	if dist > domain.MaxEditDistance(q.norm) {
		return 0, false
	}
	return q.combine(candidate, sharedTrigrams, dist, 0), true
}

func (q Query) ScorePhonetic(candidate string, sharedTrigrams int) float64 {
	return q.combine(candidate, sharedTrigrams, music.LevenshteinDistance(q.norm, candidate), 1)
}

func (q Query) combine(candidate string, sharedTrigrams, dist int, phonetic float64) float64 {
	return domain.SpellingHintsMatchScore(
		jaccardCoefficient(sharedTrigrams, len(q.trigrams), len(Trigrams(candidate))),
		editSimilarity(q.norm, candidate, dist),
		phonetic,
		lengthSimilarity(q.norm, candidate),
	)
}

func jaccardCoefficient(shared, totalA, totalB int) float64 {
	union := totalA + totalB - shared
	if union == 0 {
		return 0
	}
	return float64(shared) / float64(union)
}

func editSimilarity(a, b string, dist int) float64 {
	maxLen := max(len([]rune(a)), len([]rune(b)))
	if maxLen == 0 {
		return 0.0
	}
	return 1.0 - float64(dist)/float64(maxLen)
}

func lengthSimilarity(a, b string) float64 {
	aLen := len([]rune(a))
	bLen := len([]rune(b))
	bigger := max(aLen, bLen)
	if bigger == 0 {
		return 0.0
	}
	return 1.0 - float64(bigger-min(aLen, bLen))/float64(bigger)
}
