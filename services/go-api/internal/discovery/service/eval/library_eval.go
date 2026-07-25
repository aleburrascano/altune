package eval

import (
	"context"
	"slices"
	"strings"
	"sync/atomic"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"

	"golang.org/x/sync/errgroup"
)

type LibraryEntity struct {
	Title  string `json:"title"`
	Artist string `json:"artist"`
}

type Searcher interface {
	Search(ctx context.Context, query string) ([]domain.SearchResult, error)
}

type EvalOutcome int

const (
	EvalOutcomeUnknown EvalOutcome = iota
	EvalPass
	EvalFailWrongTop
	EvalFailNoResults
	EvalSkipped
)

func (o EvalOutcome) String() string {
	switch o {
	case EvalPass:
		return "pass"
	case EvalFailWrongTop:
		return "fail_wrong_top"
	case EvalFailNoResults:
		return "fail_no_results"
	case EvalSkipped:
		return "skipped"
	default:
		return "unknown"
	}
}

func (o EvalOutcome) MarshalJSON() ([]byte, error) {
	return []byte(`"` + o.String() + `"`), nil
}

type ResultSummary struct {
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
}

type EvalResult struct {
	Entity        LibraryEntity  `json:"entity"`
	Query         string         `json:"query"`
	Outcome       EvalOutcome    `json:"outcome"`
	MatchPosition int            `json:"match_position"`  // 0-based position the entity matched; -1 if not in top-K
	Top           *ResultSummary `json:"top,omitempty"`   // what ranked #1 when the entity wasn't #1
	Error         string         `json:"error,omitempty"` // search error, if any
}

type EvalReport struct {
	Corpus            string         `json:"corpus,omitempty"` // "" = exact, "hard" = title-only ambiguous
	K                 int            `json:"k"`                // the top-K window evaluated
	Total             int            `json:"total"`            // entities evaluated (includes skipped)
	Evaluated         int            `json:"evaluated"`        // total - skipped (the rate denominator)
	Top1Passed        int            `json:"top1_passed"`      // entity ranked #1
	TopKPassed        int            `json:"topk_passed"`      // entity within the top-K (includes top1)
	Failed            int            `json:"failed"`           // not in the top-K (or no results)
	Skipped           int            `json:"skipped"`
	FailuresByTopKind map[string]int `json:"failures_by_top_kind"` // what kind ranked #1 on a miss (incl. "none")
	Results           []EvalResult   `json:"results"`
}

func (r EvalReport) Top1Rate() float64 {
	if r.Evaluated == 0 {
		return 0
	}
	return float64(r.Top1Passed) / float64(r.Evaluated)
}

func (r EvalReport) TopKRate() float64 {
	if r.Evaluated == 0 {
		return 0
	}
	return float64(r.TopKPassed) / float64(r.Evaluated)
}

type QueryMode int

const (
	QueryExact QueryMode = iota
	QueryTitleOnly
)

func (m QueryMode) queryFor(e LibraryEntity) string {
	if m == QueryTitleOnly {
		return e.Title
	}
	return e.Artist + " " + e.Title
}

func (m QueryMode) label() string {
	if m == QueryTitleOnly {
		return "hard"
	}
	return ""
}

func RunLibraryEval(ctx context.Context, entities []LibraryEntity, searcher Searcher, concurrency, k int, progress func(done, total int)) EvalReport {
	return RunLibraryEvalMode(ctx, entities, searcher, concurrency, k, QueryExact, progress)
}

func RunLibraryEvalMode(ctx context.Context, entities []LibraryEntity, searcher Searcher, concurrency, k int, mode QueryMode, progress func(done, total int)) EvalReport {
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

	results := make([]EvalResult, total)
	var done int32
	g := new(errgroup.Group)
	g.SetLimit(concurrency)

	for i, entity := range entities {
		i, entity := i, entity
		g.Go(func() error {
			results[i] = evalOneQuery(ctx, mode.queryFor(entity), entity, searcher, k)
			n := int(atomic.AddInt32(&done, 1))
			if progress != nil && (n%step == 0 || n == total) {
				progress(n, total)
			}
			return nil
		})
	}
	_ = g.Wait()

	report := aggregate(results, k)
	report.Corpus = mode.label()
	return report
}

func evalOne(ctx context.Context, entity LibraryEntity, searcher Searcher, k int) EvalResult {
	return evalOneQuery(ctx, entity.Artist+" "+entity.Title, entity, searcher, k)
}

func evalOneQuery(ctx context.Context, query string, entity LibraryEntity, searcher Searcher, k int) EvalResult {
	if strings.TrimSpace(entity.Artist) == "" {
		return EvalResult{Entity: entity, Outcome: EvalSkipped, MatchPosition: -1}
	}

	res := EvalResult{Entity: entity, Query: query, MatchPosition: -1}

	shown, err := searcher.Search(ctx, query)
	if err != nil {
		res.Outcome = EvalFailNoResults
		res.Error = err.Error()
		return res
	}
	if len(shown) == 0 {
		res.Outcome = EvalFailNoResults
		return res
	}

	limit := k
	if limit > len(shown) {
		limit = len(shown)
	}
	for i := 0; i < limit; i++ {
		if matchesEntity(shown[i], entity) {
			res.Outcome = EvalPass
			res.MatchPosition = i
			return res
		}
	}

	res.Outcome = EvalFailWrongTop
	res.Top = &ResultSummary{
		Kind:     shown[0].Kind.String(),
		Title:    shown[0].Title,
		Subtitle: shown[0].Subtitle,
	}
	return res
}

func matchesEntity(r domain.SearchResult, entity LibraryEntity) bool {
	if r.Kind != domain.ResultKindTrack {
		return false
	}
	rt := textnorm.NormalizeForMatch(r.Title)
	et := textnorm.NormalizeForMatch(entity.Title)
	ea := textnorm.NormalizeForMatch(entity.Artist)

	if !containsTokens(rt, et) {
		return false
	}
	if ea == "" {
		raw := strings.ToLower(strings.TrimSpace(entity.Artist))
		return raw != "" &&
			(strings.Contains(strings.ToLower(r.Subtitle), raw) || strings.Contains(strings.ToLower(r.Title), raw))
	}
	return containsTokens(textnorm.NormalizeForMatch(r.Subtitle), ea) || containsTokens(rt, ea)
}

func containsTokens(have, want string) bool {
	if want == "" {
		return false
	}
	h := strings.Fields(have)
	w := strings.Fields(want)
	if len(w) > len(h) {
		return false
	}
	for i := 0; i+len(w) <= len(h); i++ {
		if slices.Equal(h[i:i+len(w)], w) {
			return true
		}
	}
	return false
}

func aggregate(results []EvalResult, k int) EvalReport {
	report := EvalReport{
		K:                 k,
		Total:             len(results),
		FailuresByTopKind: map[string]int{},
		Results:           results,
	}
	for _, res := range results {
		switch res.Outcome {
		case EvalPass:
			report.TopKPassed++
			if res.MatchPosition == 0 {
				report.Top1Passed++
			}
		case EvalSkipped:
			report.Skipped++
		case EvalFailWrongTop:
			report.Failed++
			report.FailuresByTopKind[res.Top.Kind]++
		case EvalFailNoResults:
			report.Failed++
			report.FailuresByTopKind["none"]++
		}
	}
	report.Evaluated = report.Total - report.Skipped
	return report
}
