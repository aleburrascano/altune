package service

import (
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
)

// Parameter-free relevance (requirements doc 2026-06-25 constant-free-discovery-ranking).
//
// The relevance measure used by Rank. It replaced token-sort similarity + the
// distinguishing boost (distinguishingBoostWeight = 0.35) — the lone query-fit
// constant that was left in the live path. It subsumes the boost's idea — that a
// query's rare "song" token should outweigh its repeated "artist" token — without
// a tuned knob.
//
// relevance = Σ_qtoken IDF(token) · bestFuzzyMatch(token, title+subtitle)
//                                   ────────────────────────────────────
//                                            Σ_qtoken IDF(token)
//
// - IDF(token) is the token's rarity across the candidate set (queryTokenRarity):
//   a token present in every result weighs ~0, a token naming the specific song
//   weighs ~1. Comes from the data — no tuned weight.
// - bestFuzzyMatch is the highest per-token Levenshtein ratio against the result's
//   full text — continuous, no cutoff (a cutoff would be a new query-fit constant).
// - Asymmetric: only query tokens are summed, so extra/junk tokens in the result
//   are NOT penalized. Computed over title+subtitle (not title-only — that was the
//   boost's mistake) so a canonical track carrying the artist in its subtitle
//   covers a rare artist token just as well as junk that stuffs it in the title;
//   they tie on relevance and the multi-source count ladder picks the canonical.

// idfWeightedCoverage scores how much of the query the result accounts for,
// weighting each query token by how distinguishing it is across the candidate set.
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
			continue // ubiquitous token: present everywhere, distinguishes nothing
		}
		totalWeight += w
		covered += w * bestTokenSimilarity(t, fullText)
	}

	if totalWeight == 0 {
		// Every query token is ubiquitous across the candidate set — IDF can't
		// distinguish, so coverage alone would tie an exact title with a superset
		// ("Humble" vs "Humble Beginnings") and let a more-popular wrong result win.
		// Fall back to SYMMETRIC token-sort similarity, which separates them by
		// length. Still parameter-free — a published ratio — and the switch is
		// structural (all query tokens ubiquitous), not a tuned threshold. The IDF
		// path above is unaffected (Olympics, the "artist + song" shape).
		return symmetricSimilarity(r, q)
	}
	return covered / totalWeight
}

// symmetricSimilarity is the token-sort ratio of the query against the result's
// title, and against subtitle+title — the published rapidfuzz measure, length-
// aware so an exact title beats a superset. Used only as the all-ubiquitous
// fallback in idfWeightedCoverage.
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

// bestTokenSimilarity is the highest similarity of token against any of the
// candidate tokens (its best fuzzy match in the result's text).
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

// tokenSimilarity is the normalized Levenshtein ratio between two tokens, in
// [0,1]: 1 for an exact match, degrading with edit distance. Parameter-free.
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
