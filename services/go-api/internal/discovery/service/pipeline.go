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
	return rankPipelineWith(perProvider, queryNorm, RankOptions{})
}

// RankOptions mirrors rankConfig's experiment-gated ranking inputs for callers
// outside the Service — the Mission Control re-run must rank with the same
// flag-gated stages the live search applies, or its waterfall misrepresents
// production. The zero value is the production default (every branch inert).
type RankOptions struct {
	// TailDemotion applies the tail-noise demotion predicate
	// (TAIL_DEMOTION_ENABLED).
	TailDemotion bool
	// CrossKindProminence applies the cross-kind prominence tiebreak
	// (CROSS_KIND_PROMINENCE_ENABLED).
	CrossKindProminence bool
	// Behavioral is the published satisfaction score map (nil = off), keyed by
	// result_signature (BEHAVIORAL_RANKING_ENABLED). Read-only.
	Behavioral map[string]float64
}

// config maps the exported options onto the internal rankConfig — the single
// flag→config site, so the live path and the re-run cannot diverge.
func (o RankOptions) config() rankConfig {
	cfg := rankConfig{behavioral: o.Behavioral, prominence: o.CrossKindProminence}
	if o.TailDemotion {
		cfg.demote = isLowConfidenceTail
	}
	return cfg
}

// RankWith is Rank with the experiment-gated inputs threaded in. Exported (with
// Merge and Reshape) so the re-run's stage-by-stage waterfall runs the identical
// composition the live pipeline does.
func RankWith(entities []Entity, queryNorm string, opts RankOptions) []domain.SearchResult {
	return rankWith(entities, queryNorm, opts.config())
}

// Reshape applies the post-rank list-shaping product rules in their canonical
// order (EnforceDiversity, then CollapseArtistDuplicates).
func Reshape(ranked []domain.SearchResult) []domain.SearchResult {
	return CollapseArtistDuplicates(EnforceDiversity(ranked))
}

// rankPipelineWith is rankPipeline with the experiment-gated ranking inputs
// threaded into the rank step (see RankOptions). The live search path builds the
// options from the Service's eval-gated flags; rankPipeline and the pipeline
// tests keep the zero-value default.
func rankPipelineWith(
	perProvider [][]domain.SearchResult,
	queryNorm string,
	opts RankOptions,
) []domain.SearchResult {
	return Reshape(RankWith(Merge(perProvider), queryNorm, opts))
}

// rankPipelineNoReshape is rankPipeline minus the post-rank list-shaping tier
// (EnforceDiversity + CollapseArtistDuplicates) — the eval baseline against
// which the diversity harness measures what reshaping costs (plan 2026-06-24-001).
// It is a pure read and never runs on the live search path.
func rankPipelineNoReshape(perProvider [][]domain.SearchResult, queryNorm string) []domain.SearchResult {
	return Rank(Merge(perProvider), queryNorm)
}
