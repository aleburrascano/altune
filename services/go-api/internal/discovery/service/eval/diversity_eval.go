package eval

// Diversity harness — differential on the library oracle (plan 2026-06-24-001, Phase 2).
//
// Diversity has NO ground truth for "the right amount of variety," so it is not
// measured standalone (that number would be circular — the rule mechanically
// reduces concentration, so it is always "green"). Instead the harness measures
// the rule's CONCRETE failure mode against the library oracle: the per-artist cap
// can demote the user's actual target track below the top-K fold.
//
//   - COST (gated, lower is better): run the library eval with the reshaping tier
//     ON vs OFF; the cost is the share of owned tracks that rank in the top-K
//     WITHOUT reshaping but fall out of it WITH reshaping. Right-answers
//     sacrificed to the policy.
//
//   - BENEFIT (report-only, NEVER gated): the drop in top-K artist concentration
//     (Herfindahl) that reshaping buys. You gate the collateral damage of a
//     policy; you do not gate the policy itself.
//
// Generalizes to the whole reshaping tier (EnforceDiversity + CollapseArtist
// Duplicates) — the eval seam toggles both together.

import (
	"context"
	"sync"

	"altune/go-api/internal/discovery/domain"

	"golang.org/x/sync/errgroup"
)

// VariantSearcher returns one query ranked both with and without the reshaping
// tier, from a single fan-out. Service.RankVariantsForEval satisfies it.
type VariantSearcher interface {
	SearchVariants(ctx context.Context, query string) (withReshape, withoutReshape []domain.SearchResult)
}

// DiversityResult is the per-entity differential verdict.
type DiversityResult struct {
	Entity        LibraryEntity `json:"entity"`
	Query         string        `json:"query"`
	InTopKWith    bool          `json:"in_topk_with"`    // target in top-K of the reshaped list
	InTopKWithout bool          `json:"in_topk_without"` // target in top-K of the unshaped list
}

// DiversityReport is the aggregate cost/benefit report.
type DiversityReport struct {
	Corpus               string          `json:"corpus,omitempty"` // "" = exact, "hard" = title-only
	K                    int             `json:"k"`
	Total                int             `json:"total"`
	Evaluated            int             `json:"evaluated"`             // entities scored (artist present)
	LostToReshape        int             `json:"lost_to_reshape"`       // in top-K without reshape, out with — the cost
	GainedByReshape      int             `json:"gained_by_reshape"`     // out without, in with (collapse can promote)
	ConcentrationWith    float64         `json:"concentration_with"`    // mean top-K Herfindahl, reshaped (benefit side)
	ConcentrationWithout float64         `json:"concentration_without"` // mean top-K Herfindahl, unshaped
	Losses               []FailureRecord `json:"losses"`
}

// CostRate is lost_to_reshape / evaluated, in [0,1]. Gated, lower is better.
func (r DiversityReport) CostRate() float64 {
	if r.Evaluated == 0 {
		return 0
	}
	return float64(r.LostToReshape) / float64(r.Evaluated)
}

// ConcentrationDrop is the benefit: how much top-K artist concentration the
// reshaping removed (positive = reshaping diversified). Report-only.
func (r DiversityReport) ConcentrationDrop() float64 {
	return r.ConcentrationWithout - r.ConcentrationWith
}

// RunDiversityEval scores the with/without differential for every entity using
// the exact "artist title" corpus. k is the top-K window; concurrency bounds
// live fan-out.
func RunDiversityEval(ctx context.Context, entities []LibraryEntity, vs VariantSearcher, concurrency, k int, progress func(done, total int)) DiversityReport {
	return RunDiversityEvalMode(ctx, entities, vs, concurrency, k, QueryExact, progress)
}

// RunDiversityEvalMode is RunDiversityEval with an explicit query mode — the
// hard title-only corpus is where reshaping is most likely to demote a target
// (ambiguous queries return crowded, artist-concentrated result sets).
func RunDiversityEvalMode(ctx context.Context, entities []LibraryEntity, vs VariantSearcher, concurrency, k int, mode QueryMode, progress func(done, total int)) DiversityReport {
	if concurrency < 1 {
		concurrency = 1
	}
	if k < 1 {
		k = 1
	}
	total := len(entities)
	step := total / 20
	if step < 1 {
		step = 1
	}

	results := make([]DiversityResult, total)
	concWith := make([]float64, total)
	concWithout := make([]float64, total)
	scored := make([]bool, total)

	var mu sync.Mutex
	var done int
	g := new(errgroup.Group)
	g.SetLimit(concurrency)
	for i, entity := range entities {
		i, entity := i, entity
		g.Go(func() error {
			if entity.Artist != "" {
				query := mode.queryFor(entity)
				with, without := vs.SearchVariants(ctx, query)
				results[i] = DiversityResult{
					Entity:        entity,
					Query:         query,
					InTopKWith:    entityInTopK(with, entity, k),
					InTopKWithout: entityInTopK(without, entity, k),
				}
				concWith[i] = topKConcentration(with, k)
				concWithout[i] = topKConcentration(without, k)
				scored[i] = true
			}
			mu.Lock()
			done++
			n := done
			mu.Unlock()
			if progress != nil && (n%step == 0 || n == total) {
				progress(n, total)
			}
			return nil
		})
	}
	_ = g.Wait()

	report := aggregateDiversity(results, concWith, concWithout, scored, k)
	report.Corpus = mode.label()
	return report
}

// entityInTopK reports whether the owned track matches a result within the top-K
// window — reusing the library-eval matcher (matchesEntity).
func entityInTopK(results []domain.SearchResult, entity LibraryEntity, k int) bool {
	limit := k
	if limit > len(results) {
		limit = len(results)
	}
	for i := 0; i < limit; i++ {
		if matchesEntity(results[i], entity) {
			return true
		}
	}
	return false
}

// topKConcentration is the Herfindahl index of artist (subtitle) shares within
// the top-K window: 1.0 = one artist owns the whole window, → 0 = maximally
// varied. The benefit metric — descriptive only.
func topKConcentration(results []domain.SearchResult, k int) float64 {
	limit := k
	if limit > len(results) {
		limit = len(results)
	}
	if limit == 0 {
		return 0
	}
	counts := map[string]int{}
	for i := 0; i < limit; i++ {
		key := artistKeyOf(results[i])
		counts[key]++
	}
	h := 0.0
	for _, c := range counts {
		share := float64(c) / float64(limit)
		h += share * share
	}
	return h
}

// artistKeyOf groups a result by its artist for concentration: the subtitle for
// tracks/albums, the title for artist results.
func artistKeyOf(r domain.SearchResult) string {
	if r.Kind == domain.ResultKindArtist {
		return "artist:" + r.Title
	}
	return r.Subtitle
}

func aggregateDiversity(results []DiversityResult, concWith, concWithout []float64, scored []bool, k int) DiversityReport {
	report := DiversityReport{K: k, Total: len(results), Losses: []FailureRecord{}}
	var sumWith, sumWithout float64
	for i, res := range results {
		if !scored[i] {
			continue
		}
		report.Evaluated++
		sumWith += concWith[i]
		sumWithout += concWithout[i]
		switch {
		case res.InTopKWithout && !res.InTopKWith:
			report.LostToReshape++
			attrs := QueryAttrs(res.Query)
			report.Losses = append(report.Losses, FailureRecord{Query: res.Query, Reason: "lost_to_reshape", Attrs: attrs})
		case !res.InTopKWithout && res.InTopKWith:
			report.GainedByReshape++
		}
	}
	if report.Evaluated > 0 {
		report.ConcentrationWith = sumWith / float64(report.Evaluated)
		report.ConcentrationWithout = sumWithout / float64(report.Evaluated)
	}
	return report
}
