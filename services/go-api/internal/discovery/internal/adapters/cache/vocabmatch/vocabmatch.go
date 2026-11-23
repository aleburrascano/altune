package vocabmatch

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
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
	out := make([]string, 0, len(runes)-trigramRunes+1)
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

func (q Query) Score(candidate string, sharedTrigrams int, phonetic bool) (float64, bool) {
	dist := textnorm.LevenshteinDistance(q.norm, candidate)
	if dist > maxEditDistance(q.norm) && !phonetic {
		return 0, false
	}
	return domain.VocabularyMatchScore(
		jaccardCoefficient(sharedTrigrams, len(q.trigrams), len(Trigrams(candidate))),
		editSimilarity(q.norm, candidate, dist),
		phoneticScore(phonetic),
		lengthSimilarity(q.norm, candidate),
	), true
}

func jaccardCoefficient(shared, totalA, totalB int) float64 {
	union := totalA + totalB - shared
	if union == 0 {
		return 0
	}
	return float64(shared) / float64(union)
}

func maxEditDistance(query string) int {
	n := len([]rune(query))
	if n <= 4 {
		return 1
	}
	if n <= 8 {
		return 2
	}
	return 3
}

func editSimilarity(a, b string, dist int) float64 {
	maxLen := max(len([]rune(a)), len([]rune(b)))
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
	bigger := max(aLen, bLen)
	if bigger == 0 {
		return 0.0
	}
	diff := aLen - bLen
	if diff < 0 {
		diff = -diff
	}
	return 1.0 - float64(diff)/float64(bigger)
}
