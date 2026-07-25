package service

import "altune/go-api/internal/discovery/domain"

func rankPipeline(perProvider [][]domain.SearchResult, queryNorm string) []domain.SearchResult {
	return rankPipelineWith(perProvider, queryNorm, RankOptions{})
}

type RankOptions struct {
	TailDemotion        bool
	CrossKindProminence bool
	Behavioral          map[string]float64
}

func (o RankOptions) config() rankConfig {
	cfg := rankConfig{behavioral: o.Behavioral, prominence: o.CrossKindProminence}
	if o.TailDemotion {
		cfg.demote = isLowConfidenceTail
	}
	return cfg
}

func RankWith(entities []Entity, queryNorm string, opts RankOptions) []domain.SearchResult {
	return rankWith(entities, queryNorm, opts.config())
}

type ScoredResult struct {
	Result      domain.SearchResult
	Relevance   float64
	Prominence  float64
	Behavioral  float64
	Popularity  float64
	RRF         float64
	MultiSource bool
	Demoted     bool
}

func RankExplain(entities []Entity, queryNorm string, opts RankOptions) []ScoredResult {
	scored := rankScored(entities, queryNorm, opts.config())
	out := make([]ScoredResult, len(scored))
	for i, s := range scored {
		out[i] = ScoredResult{
			Result:      s.result,
			Relevance:   s.relevance,
			Prominence:  s.prominence,
			Behavioral:  s.behavioral,
			Popularity:  s.pop,
			RRF:         s.rrf,
			MultiSource: s.multi,
			Demoted:     s.demoted,
		}
	}
	return out
}

func Reshape(ranked []domain.SearchResult) []domain.SearchResult {
	return CollapseArtistDuplicates(EnforceDiversity(ranked))
}

func rankPipelineWith(
	perProvider [][]domain.SearchResult,
	queryNorm string,
	opts RankOptions,
) []domain.SearchResult {
	return Reshape(RankWith(Merge(perProvider), queryNorm, opts))
}

func rankPipelineNoReshape(perProvider [][]domain.SearchResult, queryNorm string) []domain.SearchResult {
	return Rank(Merge(perProvider), queryNorm)
}
