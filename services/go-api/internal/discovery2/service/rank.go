package service

import (
	"sort"
	"strings"

	"altune/go-api/internal/discovery/domain"
	legacy "altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared/textnorm"
)

// Layer 3 — rank.
//
// Results are ordered by CONTINUOUS relevance — how much of the query the
// result's title (and artist) matches, via token-sort similarity — then
// popularity, then multi-source agreement, then RRF. There are deliberately no
// relevance bands, no popularity-dominance window, no kind tiers, and no intent
// contract: those were query-fit (tuned constants and pattern-specific
// machinery). A similarity measure is a published algorithm, not a fitted
// constant, and it degrades gracefully where the tier model fell off a cliff —
// a result matching more of the query's tokens simply scores higher.

// rrfK is the Reciprocal Rank Fusion constant — a published convention, used
// only as a within-tie tiebreak.
const rrfK = 60

type scored struct {
	result    domain.SearchResult
	relevance float64
	pop       float64
	rrf       float64
	multi     bool
}

// Rank applies the eligibility gates and sorts by continuous relevance,
// returning handler-ready results. queryNorm is the normalized query.
func Rank(entities []Entity, queryNorm string) []domain.SearchResult {
	q := textnorm.NormalizeForMatch(queryNorm)

	results := make([]scored, 0, len(entities))
	for _, e := range entities {
		r := e.Result
		if !sharesQueryWord(r, q) || !hasBrowseableSource(r) {
			continue
		}
		results = append(results, scored{
			result:    r,
			relevance: relevanceScore(r, q),
			pop:       popularityOf(r),
			rrf:       rrfScore(e.BestRank),
			multi:     len(providersOf(r)) > 1,
		})
	}

	sort.SliceStable(results, func(i, j int) bool { return rankLess(results[i], results[j]) })

	out := make([]domain.SearchResult, len(results))
	for i, s := range results {
		out[i] = s.result
	}
	return out
}

// rankLess orders by relevance, then popularity, then multi-source, then RRF,
// with a stable subtitle/title tiebreak.
func rankLess(a, b scored) bool {
	if a.relevance != b.relevance {
		return a.relevance > b.relevance
	}
	if a.pop != b.pop {
		return a.pop > b.pop
	}
	if a.multi != b.multi {
		return a.multi
	}
	if a.rrf != b.rrf {
		return a.rrf > b.rrf
	}
	if a.result.Subtitle != b.result.Subtitle {
		return a.result.Subtitle < b.result.Subtitle
	}
	return a.result.Title < b.result.Title
}

// relevanceScore is the token-sort similarity of the query against the result's
// title, and against artist+title — the published rapidfuzz algorithm, with no
// tuned bonuses. The better-matching of the two framings wins.
func relevanceScore(r domain.SearchResult, q string) float64 {
	if q == "" {
		return 0
	}
	title := textnorm.NormalizeForMatch(r.Title)
	best := legacy.TokenSortRatio(q, title)
	if r.Subtitle != "" {
		combined := textnorm.NormalizeForMatch(r.Subtitle + " " + r.Title)
		if s := legacy.TokenSortRatio(q, combined); s > best {
			best = s
		}
	}
	return best / 100.0
}

// sharesQueryWord drops results that share no content word with the query.
func sharesQueryWord(r domain.SearchResult, queryNorm string) bool {
	if queryNorm == "" {
		return true
	}
	hay := tokenSet(textnorm.NormalizeForMatch(r.Subtitle + " " + r.Title))
	for w := range tokenSet(queryNorm) {
		if hay[w] {
			return true
		}
	}
	return false
}

// hasBrowseableSource keeps the product rule that artist/album results need a
// Deezer source to load detail-screen content; tracks always pass.
func hasBrowseableSource(r domain.SearchResult) bool {
	if r.Kind == domain.ResultKindTrack {
		return true
	}
	for _, s := range r.Sources {
		if s.Provider == domain.ProviderDeezer {
			return true
		}
	}
	return false
}

func rrfScore(bestRank map[domain.ProviderName]int) float64 {
	s := 0.0
	for _, rank := range bestRank {
		s += 1.0 / float64(rrfK+rank)
	}
	return s
}

func tokenSet(s string) map[string]bool {
	m := make(map[string]bool)
	for _, w := range strings.Fields(s) {
		if len(w) >= 2 {
			m[w] = true
		}
	}
	return m
}
