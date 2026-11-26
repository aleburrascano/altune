package rank

import (
	"altune/go-api/internal/search/internal/core/querymatch"
	"altune/go-api/internal/search/internal/core/resultorder"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/textnorm"
	"math"
	"regexp"
	"slices"
	"sort"
)

const rrfK = 60

type demoteFunc func(domain.SearchResult) bool

func demoteNothing(domain.SearchResult) bool { return false }

type scored struct {
	result domain.SearchResult
	key    resultorder.Key
}

type rankConfig struct {
	demote     demoteFunc
	behavioral map[string]float64
	prominence bool
}

func Rank(entities []Entity, queryNorm string) []domain.SearchResult {
	return rankWith(entities, queryNorm, RankOptions{}.config())
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
		if querymatch.SharesQueryWord(e.Result, queryNorm) && hasBrowseableSource(e.Result) {
			eligible = append(eligible, e)
		}
	}

	scorer := querymatch.NewScorer(queryNorm, resultsOf(eligible))
	pickedArtist := pickedArtistName(eligible)

	results := make([]scored, 0, len(eligible))
	for _, e := range eligible {
		r := e.Result
		demoted := cfg.demote(r)
		prominence := 0.0
		if cfg.prominence {
			prominence = prominenceOf(r)
		}
		results = append(results, scored{result: r, key: resultorder.Key{
			Relevance:     scorer.Score(r),
			Behavioral:    cfg.behavioral[domain.ResultSignature(r)],
			Prominence:    prominence,
			Popularity:    r.Popularity,
			RRF:           rrfScore(e.BestRank),
			MultiProvider: len(providersOf(r)) > 1,
			Demoted:       demoted,
			TopPick:       domain.BoolExtra(r.Extras, domain.ExtraTopPick),
			NamedArtist:   isStrongNamedArtist(r, queryNorm),
			OfPick:        pickedArtist != "" && r.Kind != domain.ResultKindArtist && creditsArtist(r, pickedArtist),
			Subtitle:      r.Subtitle,
			Title:         r.Title,
		}})
	}

	sort.SliceStable(results, func(i, j int) bool { return resultorder.Less(results[i].key, results[j].key) })
	return results
}

func resultsOf(entities []Entity) []domain.SearchResult {
	out := make([]domain.SearchResult, len(entities))
	for i, e := range entities {
		out[i] = e.Result
	}
	return out
}

func pickedArtistName(eligible []Entity) string {
	for _, e := range eligible {
		if e.Result.Kind == domain.ResultKindArtist && domain.BoolExtra(e.Result.Extras, domain.ExtraTopPick) {
			return matchName(e.Result.Title)
		}
	}
	return ""
}

var creditSeparatorRe = regexp.MustCompile(`(?i)\s*(?:&|,|\s+x\s+|\b(?:featuring|feat|ft)\b\.?)\s*`)

func matchName(s string) string {
	if norm := textnorm.NormalizeForMatch(s); norm != "" {
		return norm
	}
	return textnorm.FoldName(s)
}

func creditNames(credit string) []string {
	var names []string
	for _, part := range creditSeparatorRe.Split(credit, -1) {
		if name := matchName(part); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func creditsArtist(r domain.SearchResult, artistNorm string) bool {
	return matchName(r.Subtitle) == artistNorm || slices.Contains(creditNames(r.Subtitle), artistNorm)
}

func hasBrowseableSource(r domain.SearchResult) bool {
	if r.Kind == domain.ResultKindTrack {
		return true
	}
	for _, s := range r.Sources {
		if domain.IsCanonicalContentProvider(s.Provider) {
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

func isLowConfidenceTail(r domain.SearchResult) bool {
	provs := providersOf(r)
	if len(provs) != 1 {
		return false
	}
	if !provs[domain.ProviderSoundCloud] {
		return false
	}
	return r.ISRC == "" && r.Album == ""
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

func RankPipeline(perProvider [][]domain.SearchResult, queryNorm string) []domain.SearchResult {
	return RankPipelineWith(perProvider, queryNorm, RankOptions{})
}

type RankOptions struct {
	TailDemotion        bool
	CrossKindProminence bool
	Behavioral          map[string]float64
}

func (o RankOptions) config() rankConfig {
	cfg := rankConfig{demote: demoteNothing, behavioral: o.Behavioral, prominence: o.CrossKindProminence}
	if o.TailDemotion {
		cfg.demote = isLowConfidenceTail
	}
	return cfg
}

func RankWith(entities []Entity, queryNorm string, opts RankOptions) []domain.SearchResult {
	return rankWith(entities, queryNorm, opts.config())
}

func Reshape(ranked []domain.SearchResult) []domain.SearchResult {
	return CollapseArtistDuplicates(EnforceDiversity(DropCollabArtistCards(ranked)))
}

func RankPipelineWith(
	perProvider [][]domain.SearchResult,
	queryNorm string,
	opts RankOptions,
) []domain.SearchResult {
	return Reshape(RankWith(Merge(perProvider), queryNorm, opts))
}

func RankPipelineNoReshape(perProvider [][]domain.SearchResult, queryNorm string) []domain.SearchResult {
	return Rank(Merge(perProvider), queryNorm)
}
