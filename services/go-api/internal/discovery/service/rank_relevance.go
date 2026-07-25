package service

import (
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
)

func idfWeightedCoverage(r domain.SearchResult, q string, rarity map[string]float64) float64 {
	qTokens := strings.Fields(q)
	if len(qTokens) == 0 {
		return 0
	}
	fullText := strings.Fields(textnorm.NormalizeForMatch(r.Subtitle + " " + r.Title))
	if len(fullText) == 0 {
		return 0
	}

	totalWeight, covered := 0.0, 0.0
	for _, t := range qTokens {
		w := rarity[t]
		if w <= 0 {
			continue
		}
		totalWeight += w
		covered += w * bestTokenSimilarity(t, fullText)
	}

	if totalWeight == 0 {
		return symmetricSimilarity(r, q)
	}
	return covered / totalWeight
}

func symmetricSimilarity(r domain.SearchResult, q string) float64 {
	if q == "" {
		return 0
	}
	best := textnorm.TokenSortRatio(q, textnorm.NormalizeForMatch(r.Title))
	if r.Subtitle != "" {
		combined := textnorm.NormalizeForMatch(r.Subtitle + " " + r.Title)
		if s := textnorm.TokenSortRatio(q, combined); s > best {
			best = s
		}
	}
	return best / 100.0
}

func bestTokenSimilarity(token string, candidates []string) float64 {
	best := 0.0
	for _, c := range candidates {
		if s := tokenSimilarity(token, c); s > best {
			best = s
			if best == 1 {
				break
			}
		}
	}
	return best
}

func tokenSimilarity(a, b string) float64 {
	if a == b {
		return 1
	}
	maxLen := len([]rune(a))
	if bl := len([]rune(b)); bl > maxLen {
		maxLen = bl
	}
	if maxLen == 0 {
		return 1
	}
	return 1 - float64(textnorm.LevenshteinDistance(a, b))/float64(maxLen)
}
