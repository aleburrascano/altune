package domain

import (
	"altune/go-api/internal/shared/textnorm"
	"strings"
)

const minQueryWordLen = 2

type Scorer struct {
	query  string
	tokens []string
	rarity map[string]float64
}

func NewScorer(queryNorm string, candidates []SearchResult) Scorer {
	return Scorer{query: queryNorm, tokens: strings.Fields(queryNorm), rarity: tokenRarity(queryNorm, candidates)}
}

func (s Scorer) Score(r SearchResult) float64 {
	haystack := strings.Fields(haystackOf(r))
	if len(s.tokens) == 0 || len(haystack) == 0 {
		return 0
	}
	totalWeight, covered := 0.0, 0.0
	for _, t := range s.tokens {
		w := s.rarity[t]
		totalWeight += w
		covered += w * bestTokenSimilarity(t, haystack)
	}
	if totalWeight == 0 {
		return wholeNameSimilarity(r, s.query)
	}
	return covered / totalWeight
}

func SharesQueryWord(r SearchResult, queryNorm string) bool {
	queryWords := wordSet(queryNorm)
	if len(queryWords) == 0 {
		return true
	}
	haystack := wordSet(haystackOf(r))
	for w := range queryWords {
		if haystack[w] {
			return true
		}
	}
	return false
}

func tokenRarity(queryNorm string, candidates []SearchResult) map[string]float64 {
	queryWords := wordSet(queryNorm)
	rarity := make(map[string]float64, len(queryWords))
	if len(candidates) == 0 {
		for w := range queryWords {
			rarity[w] = 1
		}
		return rarity
	}
	containing := candidatesContaining(queryWords, candidates)
	for w := range queryWords {
		rarity[w] = 1 - float64(containing[w])/float64(len(candidates))
	}
	return rarity
}

func candidatesContaining(queryWords map[string]bool, candidates []SearchResult) map[string]int {
	containing := make(map[string]int, len(queryWords))
	for _, c := range candidates {
		haystack := wordSet(haystackOf(c))
		for w := range queryWords {
			if haystack[w] {
				containing[w]++
			}
		}
	}
	return containing
}

func wholeNameSimilarity(r SearchResult, queryNorm string) float64 {
	titleOnly := textnorm.TokenSortRatio(queryNorm, textnorm.NormalizeForMatch(r.Title))
	withArtist := textnorm.TokenSortRatio(queryNorm, haystackOf(r))
	return max(titleOnly, withArtist) / 100
}

func bestTokenSimilarity(token string, candidates []string) float64 {
	best := 0.0
	for _, c := range candidates {
		best = max(best, tokenSimilarity(token, c))
	}
	return best
}

func tokenSimilarity(a, b string) float64 {
	longest := max(len([]rune(a)), len([]rune(b)), 1)
	return 1 - float64(textnorm.LevenshteinDistance(a, b))/float64(longest)
}

func haystackOf(r SearchResult) string {
	return textnorm.NormalizeForMatch(r.Subtitle + " " + r.Title)
}

func wordSet(s string) map[string]bool {
	words := make(map[string]bool)
	for _, w := range strings.Fields(s) {
		if len(w) >= minQueryWordLen {
			words[w] = true
		}
	}
	return words
}
