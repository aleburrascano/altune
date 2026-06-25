package service

import (
	"sort"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
)

// Layer 3 — rank.
//
// Results are ordered by CONTINUOUS relevance — IDF-weighted, per-token fuzzy
// coverage of the query over each result's title+subtitle (see rank_relevance.go)
// — then popularity, then multi-source agreement, then RRF. There are
// deliberately no relevance bands, no popularity-dominance window, no kind tiers,
// no intent contract, and NO tuned constant anywhere in the measure: those were
// query-fit. The relevance is parameter-free — the IDF weight comes from the data
// and the per-token match is a Levenshtein ratio — so a result accounting for more
// of the query's distinguishing tokens simply scores higher.

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
	// queryNorm is already normalized by the caller (Execute); use it directly.
	q := queryNorm

	// Pass 1: keep only eligible entities.
	eligible := make([]Entity, 0, len(entities))
	for _, e := range entities {
		if sharesQueryWord(e.Result, q) && hasBrowseableSource(e.Result) {
			eligible = append(eligible, e)
		}
	}

	// IDF weights across the candidate set: the artist portion of an "artist title"
	// query repeats across every result and so weighs ~nothing; the token that
	// names the specific song is rare and carries most of the weight. These weight
	// the relevance measure directly — there is no separate tuned bonus.
	rarity := queryTokenRarity(q, eligible)

	// Pass 2: score.
	results := make([]scored, 0, len(eligible))
	for _, e := range eligible {
		r := e.Result
		results = append(results, scored{
			result:    r,
			relevance: idfWeightedCoverage(r, q, rarity),
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

// queryTokenRarity weights each query token by how DISTINGUISHING it is across the
// candidate set: rarity = 1 - documentFrequency/N. A token present in every result
// (the artist name of an "artist title" query) approaches 0; a token that names the
// specific song approaches 1.
func queryTokenRarity(q string, eligible []Entity) map[string]float64 {
	qTokens := tokenSet(q)
	rarity := make(map[string]float64, len(qTokens))
	n := len(eligible)
	if n == 0 {
		for t := range qTokens {
			rarity[t] = 1
		}
		return rarity
	}
	df := make(map[string]int, len(qTokens))
	for _, e := range eligible {
		hay := tokenSet(textnorm.NormalizeForMatch(e.Result.Subtitle + " " + e.Result.Title))
		for t := range qTokens {
			if hay[t] {
				df[t]++
			}
		}
	}
	for t := range qTokens {
		rarity[t] = 1 - float64(df[t])/float64(n)
	}
	return rarity
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
