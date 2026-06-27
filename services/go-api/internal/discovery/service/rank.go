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

// demoteFunc flags a result for tail demotion. nil on the default path.
type demoteFunc func(domain.SearchResult) bool

type scored struct {
	result     domain.SearchResult
	relevance  float64
	behavioral float64
	pop        float64
	rrf        float64
	multi      bool
	demoted    bool
}

// Rank applies the eligibility gates and sorts by continuous relevance,
// returning handler-ready results. queryNorm is the normalized query. This is the
// default production behavior (no tail demotion) and the surface the sacred
// rank/pipeline tests assert; it delegates to rankWith with no predicate.
func Rank(entities []Entity, queryNorm string) []domain.SearchResult {
	return rankWith(entities, queryNorm, nil, nil)
}

// rankWith is Rank with an optional tail-demotion predicate. A non-nil demote
// pushes flagged results (single-source UGC/scrobble noise — see isLowConfidenceTail)
// below every non-demoted result, overriding their query-word relevance. demote is
// nil on the default path, making the demotion branch inert. EXPERIMENTAL,
// eval-gated — see docs/brainstorms/2026-06-27-discovery-tail-noise-demotion.md.
// rankWith is Rank with an optional tail-demotion predicate and an optional
// behavioral score map (keyed by result_signature). Both are nil on the default
// path, making their branches inert — the sacred rank/pipeline tests assert that
// default. behavioral is the EventConsumer-derived satisfaction signal, applied
// only as a within-tie input below relevance (see rankLess), eval A/B-gated.
func rankWith(entities []Entity, queryNorm string, demote demoteFunc, behavioral map[string]float64) []domain.SearchResult {
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
		demoted := false
		if demote != nil {
			demoted = demote(r)
		}
		results = append(results, scored{
			result:     r,
			relevance:  idfWeightedCoverage(r, q, rarity),
			behavioral: behavioral[resultSignature(r)], // nil map read → 0 (inert)
			pop:        popularityOf(r),
			rrf:        rrfScore(e.BestRank),
			multi:      len(providersOf(r)) > 1,
			demoted:    demoted,
		})
	}

	sort.SliceStable(results, func(i, j int) bool { return rankLess(results[i], results[j]) })

	out := make([]domain.SearchResult, len(results))
	for i, s := range results {
		out[i] = s.result
	}
	return out
}

// rankLess orders by demotion, then relevance, then popularity, then multi-source,
// then RRF, with a stable subtitle/title tiebreak.
func rankLess(a, b scored) bool {
	// Tail demotion (experimental, off by default): a flagged low-confidence result
	// sorts below every non-demoted one, overriding relevance. Inert when no demote
	// predicate is set — both false, so this never fires. See isLowConfidenceTail.
	if a.demoted != b.demoted {
		return !a.demoted
	}
	if a.relevance != b.relevance {
		return a.relevance > b.relevance
	}
	// Behavioral satisfaction (experimental, off by default): among equally
	// relevant results, the one users actually played-to-completion sorts above
	// the one they skip-after-click. Inert when no behavioral scores are set —
	// both 0, so this never fires. eval A/B-gated (BEHAVIORAL_RANKING_ENABLED).
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

// resultSignature is the canonical cross-query join key — (kind, normalized
// title, normalized subtitle) — identical to the one the handler emits on the
// wire and the client echoes on engagement events, so behavioral scores keyed by
// the stored signature line up with the live results being ranked.
func resultSignature(r domain.SearchResult) string {
	return r.Kind.String() + "|" +
		textnorm.NormalizeForMatch(r.Title) + "|" +
		textnorm.NormalizeForMatch(r.Subtitle)
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

// isLowConfidenceTail flags a result as tail noise: a single entry from a UGC /
// scrobble provider (SoundCloud uploads, Last.fm scrobbles) that carries no
// corroborating identity (no ISRC, MBID, or album). These dominate the result tail
// — 61% of tail positions are single-source, ~72% of that from these two providers
// (prod telemetry, Jun 2026) — and are the reupload / type-beat / reaction /
// scrobble-fragment noise users mistake for the real recording. Multi-source
// results (corroborated) and single-source results from curated catalogs
// (Deezer/iTunes/MusicBrainz) are never flagged. The demotion is uniform, so on a
// purely-underground query where every candidate is UGC it does not change relative
// order. See docs/brainstorms/2026-06-27-discovery-tail-noise-demotion.md.
// TailNoiseInTopK counts how many of the first k results are low-confidence tail
// noise (see isLowConfidenceTail) — the tail-quality signal the search log and
// discoveryeval track over time to catch result-quality regressions that
// target-recall is blind to (the noise sits below the answer).
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

func isLowConfidenceTail(r domain.SearchResult) bool {
	provs := providersOf(r)
	if len(provs) != 1 {
		return false
	}
	if !provs[domain.ProviderSoundCloud] && !provs[domain.ProviderLastFM] {
		return false
	}
	hasIdentity := stringExtra(r, "isrc") != "" ||
		stringExtra(r, "mbid") != "" ||
		stringExtra(r, "album") != ""
	return !hasIdentity
}
