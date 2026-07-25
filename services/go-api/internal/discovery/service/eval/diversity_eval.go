package eval

import (
	"context"
	"sync"

	"altune/go-api/internal/discovery/domain"

	"golang.org/x/sync/errgroup"
)

type VariantSearcher interface {
	SearchVariants(ctx context.Context, query string) (withReshape, withoutReshape []domain.SearchResult)
}

type DiversityResult struct {
	Entity        LibraryEntity `json:"entity"`
	Query         string        `json:"query"`
	InTopKWith    bool          `json:"in_topk_with"`
	InTopKWithout bool          `json:"in_topk_without"`
}

type DiversityReport struct {
	Corpus               string          `json:"corpus,omitempty"`
	K                    int             `json:"k"`
	Total                int             `json:"total"`
	Evaluated            int             `json:"evaluated"`
	LostToReshape        int             `json:"lost_to_reshape"`
	GainedByReshape      int             `json:"gained_by_reshape"`
	ConcentrationWith    float64         `json:"concentration_with"`
	ConcentrationWithout float64         `json:"concentration_without"`
	Losses               []FailureRecord `json:"losses"`
}

func (r DiversityReport) CostRate() float64 {
	if r.Evaluated == 0 {
		return 0
	}
	return float64(r.LostToReshape) / float64(r.Evaluated)
}

func (r DiversityReport) ConcentrationDrop() float64 {
	return r.ConcentrationWithout - r.ConcentrationWith
}

func RunDiversityEval(ctx context.Context, entities []LibraryEntity, vs VariantSearcher, concurrency, k int, progress func(done, total int)) DiversityReport {
	return RunDiversityEvalMode(ctx, entities, vs, concurrency, k, QueryExact, progress)
}

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
