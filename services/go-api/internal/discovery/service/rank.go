package service

import (
	"math"
	"sort"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
)

const rrfK = 60

type demoteFunc func(domain.SearchResult) bool

type scored struct {
	result     domain.SearchResult
	relevance  float64
	behavioral float64
	prominence float64
	pop        float64
	rrf        float64
	multi      bool
	demoted    bool
}

type rankConfig struct {
	demote     demoteFunc
	behavioral map[string]float64
	prominence bool
}

func Rank(entities []Entity, queryNorm string) []domain.SearchResult {
	return rankWith(entities, queryNorm, rankConfig{})
}

func rankWith(entities []Entity, queryNorm string, cfg rankConfig) []domain.SearchResult {
	results := rankScored(entities, queryNorm, cfg)
	out := make([]domain.SearchResult, len(results))
	for i, s := range results {
		out[i] = s.result
	}
	return out
}

func rankScored(entities []Entity, queryNorm string, cfg rankConfig) []scored {
	eligible := make([]Entity, 0, len(entities))
	for _, e := range entities {
		if sharesQueryWord(e.Result, queryNorm) && hasBrowseableSource(e.Result) {
			eligible = append(eligible, e)
		}
	}

	rarity := queryTokenRarity(queryNorm, eligible)

	results := make([]scored, 0, len(eligible))
	for _, e := range eligible {
		r := e.Result
		demoted := false
		if cfg.demote != nil {
			demoted = cfg.demote(r)
		}
		prominence := 0.0
		if cfg.prominence {
			prominence = prominenceOf(r)
		}
		results = append(results, scored{
			result:     r,
			relevance:  idfWeightedCoverage(r, queryNorm, rarity),
			behavioral: cfg.behavioral[domain.ResultSignature(r)],
			prominence: prominence,
			pop:        r.Popularity,
			rrf:        rrfScore(e.BestRank),
			multi:      len(providersOf(r)) > 1,
			demoted:    demoted,
		})
	}

	sort.SliceStable(results, func(i, j int) bool { return rankLess(results[i], results[j]) })
	return results
}

func rankLess(a, b scored) bool {
	if a.demoted != b.demoted {
		return !a.demoted
	}
	if a.relevance != b.relevance {
		return a.relevance > b.relevance
	}
	if a.prominence != b.prominence {
		return a.prominence > b.prominence
	}
	if a.behavioral != b.behavioral {
		return a.behavioral > b.behavioral
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

func sharesQueryWord(r domain.SearchResult, queryNorm string) bool {
	if queryNorm == "" {
		return true
	}
	qTokens := tokenSet(queryNorm)
	if len(qTokens) == 0 {
		return true
	}
	hay := tokenSet(textnorm.NormalizeForMatch(r.Subtitle + " " + r.Title))
	for w := range qTokens {
		if hay[w] {
			return true
		}
	}
	return false
}

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

func prominenceOf(r domain.SearchResult) float64 {
	raw := r.FanCount
	if r.Kind == domain.ResultKindTrack {
		raw = r.ProviderRank
	}
	if raw <= 0 {
		return 0
	}
	return math.Log1p(float64(raw))
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

func isLowConfidenceTail(r domain.SearchResult) bool {
	provs := providersOf(r)
	if len(provs) != 1 {
		return false
	}
	if !provs[domain.ProviderSoundCloud] && !provs[domain.ProviderLastFM] {
		return false
	}
	hasIdentity := r.ISRC != "" ||
		r.MBID != "" ||
		r.Album != ""
	return !hasIdentity
}

func TailNoiseInTopK(results []domain.SearchResult, k int) int {
	n := 0
	for i, r := range results {
		if i >= k {
			break
		}
		if isLowConfidenceTail(r) {
			n++
		}
	}
	return n
}
