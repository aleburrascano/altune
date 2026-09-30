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
