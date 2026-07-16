package service

import "altune/go-api/internal/discovery/domain"

// rankPipeline is the pure decision core of search: given the per-provider result
// groups and the normalized query, it resolves entities, orders them by relevance,
// and applies the list-shaping product rules — with NO ports and NO I/O. It is the
// single test surface for "do provider results turn into the right ranked list",
// exercisable end-to-end with plain data.
//
// The port-bound concerns that bracket it — identity stamping (pre-merge, reads
// the identity bridge) and display enrichment (post-rank artwork/disambiguation) —
// stay on Service.mergeRankEnrich; they fill fields, they do not decide order.
//
//	Merge                    : entity resolution (identifiers → canonical title)
//	Rank                     : continuous-relevance ordering + eligibility gates
//	EnforceDiversity         : per-artist cap within the top window (product rule)
//	CollapseArtistDuplicates : fold same-name artists into one (product rule)
func rankPipeline(perProvider [][]domain.SearchResult, queryNorm string) []domain.SearchResult {
	return rankPipelineWith(perProvider, queryNorm, rankConfig{})
}

// rankPipelineWith is rankPipeline with the experiment-gated ranking inputs
// threaded into the rank step (see rankConfig). The live search path builds the
// config from the Service's eval-gated flags; rankPipeline and the pipeline tests
// keep the zero-value default.
func rankPipelineWith(
	perProvider [][]domain.SearchResult,
	queryNorm string,
	cfg rankConfig,
) []domain.SearchResult {
	entities := Merge(perProvider)
	ranked := rankWith(entities, queryNorm, cfg)
	ranked = EnforceDiversity(ranked)
	ranked = CollapseArtistDuplicates(ranked)
	return ranked
}

// rankPipelineNoReshape is rankPipeline minus the post-rank list-shaping tier
// (EnforceDiversity + CollapseArtistDuplicates) — the eval baseline against
// which the diversity harness measures what reshaping costs (plan 2026-06-24-001).
// It is a pure read and never runs on the live search path.
func rankPipelineNoReshape(perProvider [][]domain.SearchResult, queryNorm string) []domain.SearchResult {
	return Rank(Merge(perProvider), queryNorm)
}
