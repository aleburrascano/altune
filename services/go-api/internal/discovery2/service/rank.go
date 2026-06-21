package service

import (
	"sort"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
)

// Layer 3 — disambiguate + rank.
//
// Results are ordered by categorical relevance TIERS, popularity only WITHIN a
// tier. A lower tier can never outrank a higher one, so there is no relevance
// band, no popularity-dominance window, and no additive intent boost — the
// three tuned constants of the old ranker are gone, replaced by tier categories.
//
//	T1 (exact)          — exact title, artist satisfied, kind matches intent (or none intended)
//	T2 (title/other)    — exact title, artist satisfied, but a different kind than intended
//	T3 (partial)        — partial title match
//	T4 (weak)           — shares a query word but little else
//
// Within a tier: popularity, then multi-source, then RRF. This preserves the
// genuine "popularity > multi-source" decision while structurally seating the
// same-named album (T2) immediately below the exact track (T1) — Pattern A.

// rrfK is the Reciprocal Rank Fusion constant. Kept (principled): it is the
// published convention; its role here shrinks to a within-tier tiebreak.
const rrfK = 60

// relevanceTier is the categorical relevance band (higher = more relevant).
type relevanceTier int

const (
	tierWeak           relevanceTier = iota // T4
	tierPartial                             // T3
	tierTitleOtherKind                      // T2
	tierExact                               // T1
)

type tierScored struct {
	result domain.SearchResult
	tier   relevanceTier
	pop    float64
	rrf    float64
	multi  bool
}

// Rank applies the eligibility gates and the lexicographic tier sort, returning
// handler-ready results. queryNorm is the normalized query; intent is the
// Layer-0 contract.
func Rank(entities []Entity, queryNorm string, intent Intent) []domain.SearchResult {
	target := intent.Title
	if target == "" {
		target = textnorm.NormalizeForMatch(queryNorm)
	}

	scored := make([]tierScored, 0, len(entities))
	for _, e := range entities {
		r := e.Result
		if !sharesQueryWord(r, queryNorm) {
			continue
		}
		if !hasBrowseableSource(r) {
			continue
		}
		scored = append(scored, tierScored{
			result: r,
			tier:   tierOf(r, intent, target),
			pop:    popularityOf(r),
			rrf:    rrfScore(e.BestRank),
			multi:  len(providersOf(r)) > 1,
		})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		return tierLess(scored[i], scored[j])
	})

	out := make([]domain.SearchResult, len(scored))
	for i, s := range scored {
		out[i] = s.result
	}
	return out
}

// tierLess is the lexicographic ordering: tier, then popularity, then
// multi-source, then RRF, then a stable subtitle/title tiebreak.
func tierLess(a, b tierScored) bool {
	if a.tier != b.tier {
		return a.tier > b.tier
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

func tierOf(r domain.SearchResult, intent Intent, target string) relevanceTier {
	titleExact := textnorm.NormalizeForMatch(r.Title) == target
	artistOK := intent.Artist == "" || artistMatches(r, intent.Artist)

	kindKnown := intent.Kind != domain.ResultKindUnknown
	kindMismatch := kindKnown && r.Kind != intent.Kind

	switch {
	case titleExact && artistOK && !kindMismatch:
		return tierExact
	case titleExact && artistOK && kindMismatch:
		return tierTitleOtherKind
	case partialMatch(r, target):
		return tierPartial
	default:
		return tierWeak
	}
}

func artistMatches(r domain.SearchResult, intentArtist string) bool {
	if r.Kind == domain.ResultKindArtist {
		return strings.Contains(textnorm.NormalizeForMatch(r.Title), intentArtist)
	}
	return strings.Contains(textnorm.NormalizeForMatch(r.Subtitle), intentArtist)
}

// partialMatch is true when the entity's canonical title overlaps the target as
// a substring (either direction) or the entity covers every target token —
// structural, no similarity threshold.
func partialMatch(r domain.SearchResult, target string) bool {
	core := textnorm.NormalizeForMatch(r.Title)
	if core == "" || target == "" {
		return false
	}
	if strings.Contains(core, target) || strings.Contains(target, core) {
		return true
	}
	hay := textnorm.NormalizeForMatch(r.Subtitle + " " + r.Title)
	return allTokensPresent(target, hay)
}

// sharesQueryWord drops results that share no content word with the query.
func sharesQueryWord(r domain.SearchResult, queryNorm string) bool {
	q := textnorm.NormalizeForMatch(queryNorm)
	if q == "" {
		return true
	}
	hay := tokenSet(textnorm.NormalizeForMatch(r.Subtitle + " " + r.Title))
	for w := range tokenSet(q) {
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

func allTokensPresent(target, hay string) bool {
	targetTokens := tokenSet(target)
	if len(targetTokens) == 0 {
		return false
	}
	hayTokens := tokenSet(hay)
	for w := range targetTokens {
		if !hayTokens[w] {
			return false
		}
	}
	return true
}
